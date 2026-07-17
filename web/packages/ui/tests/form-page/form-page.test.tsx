// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { FormPage } from '../../src/form-page/form-page';
import { useFormPage } from '../../src/form-page/form-page.context';

import type { FormPageContextValue } from '../../src/form-page/types';

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

// A generic, non-domain record — FormPage must not know anything about it
// beyond the injected interface.
interface Record {
  id: string;
  name: string;
}

const noop = (): void => {};

function makeValue(
  state: Partial<FormPageContextValue<Record>['state']> = {},
  actions: Partial<FormPageContextValue<Record>['actions']> = {},
  meta: Partial<FormPageContextValue<Record>['meta']> = {},
): FormPageContextValue<Record> {
  return {
    state: {
      mode: 'create',
      pending: false,
      error: null,
      canSubmit: true,
      dirty: false,
      record: null,
      recordLoading: false,
      loadError: null,
      ...state,
    },
    actions: { submit: noop, cancel: noop, ...actions },
    meta: { resourceLabel: 'widget', ...meta },
  };
}

/** A field that reads a SEPARATE resource-owned context (not the generic one). */
function NameField({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <input
      aria-label="name"
      value={value}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

/** The create variant — no Delete part composed. */
function CreateForm({ value }: { value: FormPageContextValue<Record> }) {
  const [name, setName] = useState('');
  return (
    <FormPage.Provider value={value}>
      <FormPage.Frame>
        <FormPage.Header back={<a href="/widgets">← Widgets</a>}>
          New widget
        </FormPage.Header>
        <FormPage.Body>
          <NameField value={name} onChange={setName} />
        </FormPage.Body>
        <FormPage.Actions>
          <FormPage.Cancel>Cancel</FormPage.Cancel>
          <FormPage.Submit>Create widget</FormPage.Submit>
        </FormPage.Actions>
      </FormPage.Frame>
    </FormPage.Provider>
  );
}

/** The edit variant — same parts + Delete, no boolean toggled. */
function EditForm({ value }: { value: FormPageContextValue<Record> }) {
  return (
    <FormPage.Provider value={value}>
      <FormPage.Frame>
        <FormPage.Header>Edit widget</FormPage.Header>
        <FormPage.Body>
          <NameField value="seed" onChange={noop} />
        </FormPage.Body>
        <FormPage.Actions>
          <FormPage.Delete>Delete widget</FormPage.Delete>
          <FormPage.Cancel>Cancel</FormPage.Cancel>
          <FormPage.Submit>Save changes</FormPage.Submit>
        </FormPage.Actions>
      </FormPage.Frame>
    </FormPage.Provider>
  );
}

describe('FormPage — compound composition', () => {
  it('renders the header title + composed back link', () => {
    render(<CreateForm value={makeValue()} />);
    expect(screen.getByRole('heading', { name: 'New widget' })).toBeDefined();
    expect(screen.getByRole('link', { name: '← Widgets' })).toBeDefined();
  });

  it('renders the body fields (children, no render prop)', () => {
    render(<CreateForm value={makeValue()} />);
    expect(screen.getByLabelText('name')).toBeDefined();
  });
});

describe('FormPage — submit wiring', () => {
  it('calls injected submit on native form submit (Enter / button)', () => {
    const submit = vi.fn();
    render(<CreateForm value={makeValue({}, { submit })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Create widget' }));
    expect(submit).toHaveBeenCalledTimes(1);
  });

  it('disables Submit when canSubmit is false', () => {
    render(<CreateForm value={makeValue({ canSubmit: false })} />);
    expect(
      screen.getByRole('button', { name: 'Create widget' }).hasAttribute('disabled'),
    ).toBe(true);
  });

  it('shows "Saving…" and disables Submit while pending', () => {
    render(<CreateForm value={makeValue({ pending: true })} />);
    expect(screen.getByRole('button', { name: 'Saving…' })).toBeDefined();
    expect(
      screen.getByRole('button', { name: 'Saving…' }).hasAttribute('disabled'),
    ).toBe(true);
  });

  it('renders state.error inline in Actions', () => {
    render(<CreateForm value={makeValue({ error: 'Boom' })} />);
    expect(screen.getByText('Boom')).toBeDefined();
  });
});

describe('FormPage — cancel + dirty guard', () => {
  it('cancels immediately when the form is clean', () => {
    const cancel = vi.fn();
    render(<CreateForm value={makeValue({ dirty: false }, { cancel })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(cancel).toHaveBeenCalledTimes(1);
  });

  it('routes cancel through an unsaved-changes confirm when dirty', () => {
    const cancel = vi.fn();
    render(<CreateForm value={makeValue({ dirty: true }, { cancel })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    // Not abandoned yet — the confirm intercepts.
    expect(cancel).not.toHaveBeenCalled();
    expect(screen.getByText('Discard unsaved changes?')).toBeDefined();
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
    expect(cancel).toHaveBeenCalledTimes(1);
  });

  it('disables Cancel while a write is pending', () => {
    render(<CreateForm value={makeValue({ pending: true })} />);
    expect(
      screen.getByRole('button', { name: 'Cancel' }).hasAttribute('disabled'),
    ).toBe(true);
  });

  it('reports the derived dirty signal to meta.onDirtyChange', () => {
    const onDirtyChange = vi.fn();
    render(<CreateForm value={makeValue({ dirty: true }, {}, { onDirtyChange })} />);
    expect(onDirtyChange).toHaveBeenCalledWith(true);
  });
});

describe('FormPage — delete by composition (edit only)', () => {
  it('renders the Delete button only in the edit tree and wires actions.delete', () => {
    const del = vi.fn();
    render(<EditForm value={makeValue({ mode: 'edit' }, { delete: del })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Delete widget' }));
    expect(del).toHaveBeenCalledTimes(1);
  });

  it('has no Delete affordance in the create tree', () => {
    render(<CreateForm value={makeValue()} />);
    expect(screen.queryByRole('button', { name: /delete/i })).toBeNull();
  });

  it('renders nothing for Delete when no delete action is injected', () => {
    // Defensive: edit tree composed but delete undefined → button absent.
    render(<EditForm value={makeValue({ mode: 'edit' }, {})} />);
    expect(screen.queryByRole('button', { name: 'Delete widget' })).toBeNull();
  });
});

describe('FormPage — dependency injection', () => {
  it('throws when a part is used outside a provider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    function Bare() {
      useFormPage();
      return null;
    }
    expect(() => render(<Bare />)).toThrow(/within a <FormPage.Provider>/);
    spy.mockRestore();
  });
});
