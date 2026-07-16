// @vitest-environment jsdom
import { fireEvent, render, screen, within } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { SecretsAdmin } from '../../src/resource-admin/secrets-admin';

// Radix Checkbox measures the DOM; jsdom needs a ResizeObserver shim.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

import type {
  Secret,
  SecretsAdminContextValue,
} from '../../src/resource-admin/types';

const secret: Secret = {
  name: 'organizations/acme/secrets/stripe-key',
  displayName: 'Stripe key',
  createTime: '2026-01-01T00:00:00Z',
  updateTime: '2026-02-01T00:00:00Z',
  etag: 'e1',
};

const noop = (): void => {};

function makeValue(
  overrides: Partial<SecretsAdminContextValue['state']>,
): SecretsAdminContextValue {
  return {
    state: {
      secrets: [secret],
      isLoading: false,
      loadError: null,
      dialog: {
        open: false,
        mode: 'create',
        editing: null,
        error: null,
        pending: false,
      },
      remove: { target: null, error: null, pending: false },
      ...overrides,
    },
    actions: {
      openCreate: noop,
      openEdit: noop,
      closeDialog: noop,
      submit: noop,
      openRemove: noop,
      closeRemove: noop,
      confirmRemove: noop,
    },
  };
}

function renderAdmin(
  overrides: Partial<SecretsAdminContextValue['state']>,
): void {
  render(
    <SecretsAdmin.Provider value={makeValue(overrides)}>
      <SecretsAdmin.Root />
    </SecretsAdmin.Provider>,
  );
}

describe('SecretsAdmin — set-only invariant', () => {
  it('never renders a value field or input in the list/table', () => {
    renderAdmin({});
    // The secret is listed by metadata only …
    expect(screen.getByText('Stripe key')).toBeDefined();
    // … and no value is ever surfaced: no inputs at all in the read view.
    expect(document.querySelector('input')).toBeNull();
    expect(document.querySelector('input[type="password"]')).toBeNull();
  });

  it('hides the value field when editing an existing secret until Rotate is ticked', () => {
    renderAdmin({
      dialog: {
        open: true,
        mode: 'edit',
        editing: secret,
        error: null,
        pending: false,
      },
    });

    // Set-only: no value field is shown for an existing secret by default.
    expect(screen.queryByText('New value')).toBeNull();
    expect(document.querySelector('input[type="password"]')).toBeNull();

    // Ticking "Rotate value" reveals the write-only value input.
    fireEvent.click(screen.getByRole('checkbox', { name: 'Rotate value' }));

    expect(screen.getByText('New value')).toBeDefined();
    expect(document.querySelector('input[type="password"]')).not.toBeNull();
  });

  it('requires the value on create (shown, no rotate toggle)', () => {
    renderAdmin({
      dialog: {
        open: true,
        mode: 'create',
        editing: null,
        error: null,
        pending: false,
      },
    });

    expect(screen.getByText('Value')).toBeDefined();
    expect(document.querySelector('input[type="password"]')).not.toBeNull();
    // No rotate toggle on create — the value is always written.
    expect(screen.queryByRole('checkbox', { name: 'Rotate value' })).toBeNull();
  });
});

describe('SecretsAdmin — table', () => {
  it('renders generic empty copy without the secret()-reference hint', () => {
    renderAdmin({ secrets: [] });
    expect(screen.getByText('No secrets yet.')).toBeDefined();
    // The old round's "reference it from a connector" copy is gone.
    expect(screen.queryByText(/reference it from a connector/)).toBeNull();
  });

  it('renders edit/delete as icon actions, with destructive styling on delete', () => {
    const openEdit = vi.fn();
    const openRemove = vi.fn();
    const value = makeValue({ secrets: [secret] });
    value.actions.openEdit = openEdit;
    value.actions.openRemove = openRemove;
    render(
      <SecretsAdmin.Provider value={value}>
        <SecretsAdmin.Root />
      </SecretsAdmin.Provider>,
    );

    const edit = screen.getByRole('button', { name: 'Edit secret' });
    const remove = screen.getByRole('button', { name: 'Delete secret' });
    expect(edit.textContent).toBe('');
    expect(remove.textContent).toBe('');
    expect(remove.className).toContain('text-destructive');

    fireEvent.click(edit);
    expect(openEdit).toHaveBeenCalledWith(secret);
    fireEvent.click(remove);
    expect(openRemove).toHaveBeenCalledWith(secret);
  });
});

describe('SecretsAdmin — password-manager suppression', () => {
  it('opts the value field out of password managers with a neutral name', () => {
    renderAdmin({
      secrets: [],
      dialog: {
        open: true,
        mode: 'create',
        editing: null,
        error: null,
        pending: false,
      },
    });
    const dialog = screen.getByRole('dialog');
    const input = dialog.querySelector(
      'input[type="password"]',
    ) as HTMLInputElement;

    expect(input.getAttribute('autocomplete')).toBe('off');
    expect(input.getAttribute('data-1p-ignore')).toBe('true');
    expect(input.getAttribute('data-lpignore')).toBe('true');
    expect(input.getAttribute('data-bwignore')).toBe('true');
    // name/id must not contain "password" (managers key off that too).
    expect(input.getAttribute('name')).toBe('secret-value');
    expect(input.getAttribute('id')).toBe('secret-value');
    expect(input.getAttribute('name')).not.toMatch(/password/i);
    expect(input.getAttribute('id')).not.toMatch(/password/i);
  });
});

describe('SecretsAdmin — value visibility toggle', () => {
  it('masks the value by default and reveals it on toggle', () => {
    renderAdmin({
      secrets: [],
      dialog: {
        open: true,
        mode: 'create',
        editing: null,
        error: null,
        pending: false,
      },
    });
    const dialog = screen.getByRole('dialog');

    // Hidden by default.
    expect(dialog.querySelector('input[type="password"]')).not.toBeNull();
    expect(dialog.querySelector('input[type="text"]')).toBeNull();

    // The eye toggle flips the input to visible text …
    fireEvent.click(within(dialog).getByRole('button', { name: 'Show value' }));
    expect(dialog.querySelector('input[type="password"]')).toBeNull();
    expect(dialog.querySelector('input[type="text"]')).not.toBeNull();

    // … and back to hidden.
    fireEvent.click(within(dialog).getByRole('button', { name: 'Hide value' }));
    expect(dialog.querySelector('input[type="password"]')).not.toBeNull();
  });
});

describe('SecretsAdmin — form action gating', () => {
  it('a fresh, empty create dialog disables Submit (not "Saving…") but keeps Cancel live', () => {
    const closeDialog = vi.fn();
    const value = makeValue({
      dialog: {
        open: true,
        mode: 'create',
        editing: null,
        error: null,
        pending: false,
      },
    });
    value.actions.closeDialog = closeDialog;

    render(
      <SecretsAdmin.Provider value={value}>
        <SecretsAdmin.Root />
      </SecretsAdmin.Provider>,
    );

    // Submit is disabled because the form is empty — NOT because it's saving.
    const submit = screen.getByRole('button', {
      name: 'Create secret',
    }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    expect(submit.textContent).toBe('Create secret');
    expect(submit.textContent).not.toBe('Saving…');

    // Cancel is never gated on form validity — it must stay clickable.
    const cancel = screen.getByRole('button', {
      name: 'Cancel',
    }) as HTMLButtonElement;
    expect(cancel.disabled).toBe(false);

    fireEvent.click(cancel);
    expect(closeDialog).toHaveBeenCalledTimes(1);
  });
});

describe('SecretsAdmin — auto-derived identifier', () => {
  const createDialog = {
    open: true,
    mode: 'create' as const,
    editing: null,
    error: null,
    pending: false,
  };

  it('derives the identifier slug from the display name', () => {
    renderAdmin({ secrets: [], dialog: createDialog });
    const dialog = screen.getByRole('dialog');

    const displayName = within(dialog).getAllByRole('textbox')[0];
    fireEvent.change(displayName, { target: { value: 'My API Key' } });

    // Shown read-only as the derived slug.
    expect(within(dialog).getByText('my-api-key')).toBeDefined();
  });

  it('lets the user override the identifier, which then stops re-deriving', () => {
    renderAdmin({ secrets: [], dialog: createDialog });
    const dialog = screen.getByRole('dialog');

    const displayName = within(dialog).getAllByRole('textbox')[0];
    fireEvent.change(displayName, { target: { value: 'My API Key' } });
    expect(within(dialog).getByText('my-api-key')).toBeDefined();

    // Reveal the editable identifier and override it.
    fireEvent.click(within(dialog).getByRole('button', { name: 'Edit' }));
    const idInput = within(dialog).getAllByRole('textbox')[1] as HTMLInputElement;
    fireEvent.change(idInput, { target: { value: 'custom-id' } });

    // Further display-name edits no longer clobber the override.
    fireEvent.change(displayName, { target: { value: 'Totally Different' } });
    expect(idInput.value).toBe('custom-id');
  });
});
