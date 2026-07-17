// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import {
  ConnectorCreateFields,
  ConnectorEditFields,
} from '../../src/resource-admin/connector-fields';
import { ConnectorFormProvider } from '../../src/resource-admin/connector-form-provider';
import { ResourceFormPage } from '../../src/resource-admin/resource-form-page';

import type { Connector } from '../../src/resource-admin/types';

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

const noop = (): void => {};

const spaceOptions = [
  { name: 'organizations/acme/spaces/main', slug: 'main', displayName: 'Main' },
];

const editRecord: Connector = {
  name: 'organizations/acme/spaces/main/connectors/vizrt',
  displayName: 'VizRT',
  http: { baseUrl: 'https://vizrt.example.com' },
  updateTime: '2026-02-01T00:00:00Z',
};

function renderCreate(overrides?: {
  mutate?: (values: unknown) => void;
  onCancel?: () => void;
}) {
  render(
    <ConnectorFormProvider
      mode="create"
      record={null}
      recordLoading={false}
      loadError={null}
      pending={false}
      error={null}
      mutate={overrides?.mutate ?? noop}
      onCancel={overrides?.onCancel ?? noop}
      spaceOptions={spaceOptions}
      agentOptions={[]}
    >
      <ResourceFormPage.Create>
        <ConnectorCreateFields />
      </ResourceFormPage.Create>
    </ConnectorFormProvider>,
  );
}

function renderEdit(overrides?: {
  mutate?: (values: unknown) => void;
  onDelete?: () => void;
}) {
  render(
    <ConnectorFormProvider
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
      agentOptions={[]}
    >
      <ResourceFormPage.Edit>
        <ConnectorEditFields />
      </ResourceFormPage.Edit>
    </ConnectorFormProvider>,
  );
}

describe('ConnectorFormPage — create variant', () => {
  it('renders the standard "New connector" title + "Create connector" submit', () => {
    renderCreate();
    expect(screen.getByRole('heading', { name: 'New connector' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Create connector' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDefined();
  });

  it('composes NO delete affordance in the create tree', () => {
    renderCreate();
    expect(screen.queryByRole('button', { name: /delete/i })).toBeNull();
  });

  it('renders the identifier + scope PICKER + HTTP config fields', () => {
    renderCreate();
    expect(screen.getByText('Identifier')).toBeDefined();
    // Org-rollup create renders the scope picker (resting on its unset placeholder).
    expect(
      screen.getByPlaceholderText('No space — organization'),
    ).toBeDefined();
    expect(screen.getByText('Base URL')).toBeDefined();
    expect(screen.getByText('Headers')).toBeDefined();
    expect(screen.getByText('Run on Agent')).toBeDefined();
  });

  it('derives the identifier slug from the display name', () => {
    renderCreate();
    const displayName = screen.getAllByRole('textbox')[0];
    fireEvent.change(displayName, { target: { value: 'Stripe Payments' } });
    expect(screen.getByText('stripe-payments')).toBeDefined();
  });

  it('gates submit on a valid identifier + base URL, then submits the values', () => {
    const mutate = vi.fn();
    renderCreate({ mutate });
    const submit = screen.getByRole('button', { name: 'Create connector' });
    // Nothing filled → invalid → disabled.
    expect(submit.hasAttribute('disabled')).toBe(true);

    fireEvent.change(screen.getAllByRole('textbox')[0], {
      target: { value: 'Stripe' },
    });
    fireEvent.change(screen.getByPlaceholderText('https://api.example.com'), {
      target: { value: 'https://api.stripe.com' },
    });
    expect(submit.hasAttribute('disabled')).toBe(false);

    fireEvent.click(submit);
    expect(mutate).toHaveBeenCalledTimes(1);
    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        connectorId: 'stripe',
        displayName: 'Stripe',
        baseUrl: 'https://api.stripe.com',
        scope: '',
      }),
    );
  });
});

describe('ConnectorFormPage — edit variant', () => {
  it('renders the "Edit connector" title, "Save changes" submit, and Delete', () => {
    renderEdit();
    expect(screen.getByRole('heading', { name: 'Edit connector' })).toBeDefined();
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeDefined();
    expect(
      screen.getByRole('button', { name: 'Delete connector' }),
    ).toBeDefined();
  });

  it('shows scope read-only and renders NO identifier field on edit', () => {
    renderEdit();
    // Scope is immutable — a disabled input showing the resolved space label.
    const scope = screen.getByDisplayValue('Main');
    expect(scope.hasAttribute('disabled')).toBe(true);
    expect(screen.queryByText('Identifier')).toBeNull();
  });

  it('seeds the fields from the edit record', () => {
    renderEdit();
    expect(screen.getByDisplayValue('VizRT')).toBeDefined();
    expect(screen.getByDisplayValue('https://vizrt.example.com')).toBeDefined();
  });

  it('opens the delete flow through the injected actions.delete', () => {
    const onDelete = vi.fn();
    renderEdit({ onDelete });
    fireEvent.click(screen.getByRole('button', { name: 'Delete connector' }));
    expect(onDelete).toHaveBeenCalledTimes(1);
  });

  it('submits the edited values (Save changes is enabled with a valid config)', () => {
    const mutate = vi.fn();
    renderEdit({ mutate });
    const submit = screen.getByRole('button', { name: 'Save changes' });
    expect(submit.hasAttribute('disabled')).toBe(false);
    fireEvent.click(submit);
    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        displayName: 'VizRT',
        baseUrl: 'https://vizrt.example.com',
      }),
    );
  });
});

describe('ConnectorFormPage — edit load states', () => {
  it('shows a loading notice while the record loads', () => {
    render(
      <ConnectorFormProvider
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
        agentOptions={[]}
      >
        <ResourceFormPage.Edit>
          <ConnectorEditFields />
        </ResourceFormPage.Edit>
      </ConnectorFormProvider>,
    );
    expect(screen.getByText('Loading connector…')).toBeDefined();
    expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();
  });

  it('shows the load-error notice on a failed record fetch', () => {
    render(
      <ConnectorFormProvider
        mode="edit"
        record={null}
        recordLoading={false}
        loadError="Couldn't load this connector."
        pending={false}
        error={null}
        mutate={noop}
        onCancel={noop}
        onDelete={noop}
        spaceOptions={spaceOptions}
        agentOptions={[]}
      >
        <ResourceFormPage.Edit>
          <ConnectorEditFields />
        </ResourceFormPage.Edit>
      </ConnectorFormProvider>,
    );
    expect(screen.getByText("Couldn't load this connector.")).toBeDefined();
  });
});

describe('ConnectorFormPage — dirty state', () => {
  it('is not dirty on first render, dirty after an edit (drives the guard)', () => {
    const onCancel = vi.fn();
    render(
      <ConnectorFormProvider
        mode="edit"
        record={editRecord}
        recordLoading={false}
        loadError={null}
        pending={false}
        error={null}
        mutate={noop}
        onCancel={onCancel}
        onDelete={noop}
        spaceOptions={spaceOptions}
        agentOptions={[]}
      >
        <ResourceFormPage.Edit>
          <ConnectorEditFields />
        </ResourceFormPage.Edit>
      </ConnectorFormProvider>,
    );
    // Clean → Cancel goes straight through (no confirm dialog).
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('Discard unsaved changes?')).toBeNull();

    // Edit a field → dirty → Cancel now routes through the confirm.
    fireEvent.change(screen.getByDisplayValue('VizRT'), {
      target: { value: 'VizRT Renamed' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    const dialog = screen.getByText('Discard unsaved changes?');
    expect(dialog).toBeDefined();
    // onCancel not fired again until the user confirms discard.
    expect(onCancel).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
    expect(onCancel).toHaveBeenCalledTimes(2);
  });
});
