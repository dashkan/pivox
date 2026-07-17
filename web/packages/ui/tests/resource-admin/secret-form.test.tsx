// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import {
  SecretCreateFields,
  SecretEditFields,
} from '../../src/resource-admin/secret-fields';
import { SecretFormProvider } from '../../src/resource-admin/secret-form-provider';
import { ResourceFormPage } from '../../src/resource-admin/resource-form-page';

import type { Secret } from '../../src/resource-admin/types';

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

afterEach(cleanup);

const noop = (): void => {};

const spaceOptions = [
  { name: 'organizations/acme/spaces/main', slug: 'main', displayName: 'Main' },
];

const editRecord: Secret = {
  name: 'organizations/acme/spaces/main/secrets/vizrt-key',
  displayName: 'VizRT key',
  createTime: '2026-01-01T00:00:00Z',
  updateTime: '2026-02-01T00:00:00Z',
  etag: 'e1',
};

function renderCreate(overrides?: {
  mutate?: (values: unknown) => void;
  onCancel?: () => void;
}) {
  render(
    <SecretFormProvider
      mode="create"
      record={null}
      recordLoading={false}
      loadError={null}
      pending={false}
      error={null}
      mutate={overrides?.mutate ?? noop}
      onCancel={overrides?.onCancel ?? noop}
      spaceOptions={spaceOptions}
    >
      <ResourceFormPage.Create>
        <SecretCreateFields />
      </ResourceFormPage.Create>
    </SecretFormProvider>,
  );
}

function renderEdit(overrides?: {
  mutate?: (values: unknown) => void;
  onDelete?: () => void;
}) {
  render(
    <SecretFormProvider
      mode="edit"
      record={editRecord}
      recordLoading={false}
      loadError={null}
      pending={false}
      error={null}
      mutate={overrides?.mutate ?? noop}
      onCancel={noop}
      onDelete={overrides?.onDelete ?? noop}
      spaceOptions={spaceOptions}
    >
      <ResourceFormPage.Edit>
        <SecretEditFields />
      </ResourceFormPage.Edit>
    </SecretFormProvider>,
  );
}

describe('SecretFormPage — create variant', () => {
  it('renders the standard "New secret" title + "Create secret" submit', () => {
    renderCreate();
    expect(screen.getByRole('heading', { name: 'New secret' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Create secret' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDefined();
  });

  it('composes NO delete affordance in the create tree', () => {
    renderCreate();
    expect(screen.queryByRole('button', { name: /delete/i })).toBeNull();
  });

  it('renders identifier + scope PICKER + required value field (masked, no rotate toggle)', () => {
    renderCreate();
    expect(screen.getByText('Identifier')).toBeDefined();
    expect(screen.getByPlaceholderText('No space — organization')).toBeDefined();
    // Value is required and shown on create; masked by default.
    expect(screen.getByText('Value')).toBeDefined();
    expect(document.querySelector('input[type="password"]')).not.toBeNull();
    // No rotate toggle on create — the value is always written.
    expect(screen.queryByRole('checkbox', { name: 'Rotate value' })).toBeNull();
  });

  it('derives the identifier slug from the display name', () => {
    renderCreate();
    fireEvent.change(screen.getAllByRole('textbox')[0], {
      target: { value: 'My API Key' },
    });
    expect(screen.getByText('my-api-key')).toBeDefined();
  });

  it('opts the value field out of password managers with a neutral name', () => {
    renderCreate();
    const input = document.querySelector(
      'input[type="password"]',
    ) as HTMLInputElement;
    expect(input.getAttribute('autocomplete')).toBe('off');
    expect(input.getAttribute('data-1p-ignore')).toBe('true');
    expect(input.getAttribute('data-lpignore')).toBe('true');
    expect(input.getAttribute('data-bwignore')).toBe('true');
    expect(input.getAttribute('name')).toBe('secret-value');
    expect(input.getAttribute('name')).not.toMatch(/password/i);
  });

  it('masks the value by default and reveals it on the eye toggle', () => {
    renderCreate();
    expect(document.querySelector('input[type="password"]')).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Show value' }));
    expect(document.querySelector('input[type="text"]')).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Hide value' }));
    expect(document.querySelector('input[type="password"]')).not.toBeNull();
  });

  it('gates submit on a valid identifier + value, then submits the values', () => {
    const mutate = vi.fn();
    renderCreate({ mutate });
    const submit = screen.getByRole('button', { name: 'Create secret' });
    // Nothing filled → invalid → disabled.
    expect(submit.hasAttribute('disabled')).toBe(true);

    fireEvent.change(screen.getAllByRole('textbox')[0], {
      target: { value: 'Stripe key' },
    });
    fireEvent.change(document.querySelector('#secret-value') as HTMLInputElement, {
      target: { value: 's3cr3t' },
    });
    expect(submit.hasAttribute('disabled')).toBe(false);

    fireEvent.click(submit);
    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        secretId: 'stripe-key',
        displayName: 'Stripe key',
        value: 's3cr3t',
        scope: '',
      }),
    );
  });
});

describe('SecretFormPage — edit variant', () => {
  it('renders the "Edit secret" title, "Save changes" submit, and Delete', () => {
    renderEdit();
    expect(screen.getByRole('heading', { name: 'Edit secret' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Delete secret' })).toBeDefined();
  });

  it('shows scope read-only and renders NO identifier field on edit', () => {
    renderEdit();
    const scope = screen.getByDisplayValue('Main');
    expect(scope.hasAttribute('disabled')).toBe(true);
    expect(screen.queryByText('Identifier')).toBeNull();
  });

  it('hides the value field until Rotate is ticked (set-only invariant)', () => {
    renderEdit();
    // No value field for an existing secret by default.
    expect(screen.queryByText('New value')).toBeNull();
    expect(document.querySelector('input[type="password"]')).toBeNull();

    fireEvent.click(screen.getByRole('checkbox', { name: 'Rotate value' }));
    expect(screen.getByText('New value')).toBeDefined();
    expect(document.querySelector('input[type="password"]')).not.toBeNull();
  });

  it('opens the delete flow through the injected actions.delete', () => {
    const onDelete = vi.fn();
    renderEdit({ onDelete });
    fireEvent.click(screen.getByRole('button', { name: 'Delete secret' }));
    expect(onDelete).toHaveBeenCalledTimes(1);
  });

  it('submits a metadata-only edit (rotate off) with Save changes enabled', () => {
    const mutate = vi.fn();
    renderEdit({ mutate });
    const submit = screen.getByRole('button', { name: 'Save changes' });
    // Metadata-only edit needs no value → enabled immediately.
    expect(submit.hasAttribute('disabled')).toBe(false);
    fireEvent.click(submit);
    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({ displayName: 'VizRT key', rotate: false }),
    );
  });

  it('requires the new value once Rotate is ticked (gates submit)', () => {
    renderEdit();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Rotate value' }));
    const submit = screen.getByRole('button', { name: 'Save changes' });
    // Rotate on + empty value → invalid.
    expect(submit.hasAttribute('disabled')).toBe(true);
    fireEvent.change(document.querySelector('#secret-value') as HTMLInputElement, {
      target: { value: 'rotated' },
    });
    expect(submit.hasAttribute('disabled')).toBe(false);
  });
});

describe('SecretFormPage — edit load states', () => {
  it('shows a loading notice while the record loads', () => {
    render(
      <SecretFormProvider
        mode="edit"
        record={null}
        recordLoading={true}
        loadError={null}
        pending={false}
        error={null}
        mutate={noop}
        onCancel={noop}
        onDelete={noop}
        spaceOptions={spaceOptions}
      >
        <ResourceFormPage.Edit>
          <SecretEditFields />
        </ResourceFormPage.Edit>
      </SecretFormProvider>,
    );
    expect(screen.getByText('Loading secret…')).toBeDefined();
    expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();
  });
});
