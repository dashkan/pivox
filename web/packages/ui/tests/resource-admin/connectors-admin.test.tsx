// @vitest-environment jsdom
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { beforeAll, describe, expect, it } from 'vitest';

import { ConnectorsAdmin } from '../../src/resource-admin/connectors-admin';

import type {
  Connector,
  ConnectorsAdminContextValue,
} from '../../src/resource-admin/types';

// Radix Select + Base UI combobox measure/scroll the DOM; jsdom needs shims.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

const noop = (): void => {};

function makeValue(
  overrides: Partial<ConnectorsAdminContextValue['state']>,
): ConnectorsAdminContextValue {
  return {
    state: {
      connectors: [],
      isLoading: false,
      loadError: null,
      agentOptions: [],
      dialog: {
        open: false,
        mode: 'create',
        editing: null,
        error: null,
        pending: false,
      },
      remove: { target: null, error: null, pending: false },
      filters: {},
      sort: null,
      pageSize: 25,
      scope: '',
      spaceOptions: [],
      agentsInUse: [],
      pagination: { hasPrevPage: false, hasNextPage: false },
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
      setFilter: noop,
      clearFilters: noop,
      toggleSort: noop,
      setPageSize: noop,
      setScope: noop,
      nextPage: noop,
      prevPage: noop,
    },
  };
}

const connector: Connector = {
  name: 'organizations/acme/connectors/stripe',
  displayName: 'Stripe',
  http: { baseUrl: 'https://api.stripe.com' },
  updateTime: '2026-02-01T00:00:00Z',
};

const spaceConnector: Connector = {
  name: 'organizations/acme/spaces/main/connectors/vizrt',
  displayName: 'VizRT',
  http: { baseUrl: 'https://vizrt.example.com' },
  updateTime: '2026-02-01T00:00:00Z',
};

const spaceOptions = [
  { name: 'organizations/acme/spaces/main', slug: 'main', displayName: 'Main' },
];

function renderTable(
  overrides: Partial<ConnectorsAdminContextValue['state']>,
  actions?: Partial<ConnectorsAdminContextValue['actions']>,
): ConnectorsAdminContextValue {
  const value = makeValue(overrides);
  if (actions) Object.assign(value.actions, actions);
  render(
    <ConnectorsAdmin.Provider value={value}>
      <ConnectorsAdmin.Root />
    </ConnectorsAdmin.Provider>,
  );
  return value;
}

function renderCreate(): HTMLElement {
  render(
    <ConnectorsAdmin.Provider
      value={makeValue({
        connectors: [],
        dialog: {
          open: true,
          mode: 'create',
          editing: null,
          error: null,
          pending: false,
        },
      })}
    >
      <ConnectorsAdmin.Root />
    </ConnectorsAdmin.Provider>,
  );
  return screen.getByRole('dialog');
}

function renderEdit(editing: Connector): HTMLElement {
  render(
    <ConnectorsAdmin.Provider
      value={makeValue({
        connectors: [],
        spaceOptions,
        dialog: { open: true, mode: 'edit', editing, error: null, pending: false },
      })}
    >
      <ConnectorsAdmin.Root />
    </ConnectorsAdmin.Provider>,
  );
  return screen.getByRole('dialog');
}

describe('ConnectorsAdmin — type selector', () => {
  it('defaults the Type to HTTP and renders the HTTP variant fields', () => {
    const dialog = renderCreate();
    expect(within(dialog).getByText('Type')).toBeDefined();
    // The Type trigger reflects the HTTP default (found by content, since Scope
    // and Run-on-Agent are also comboboxes in the form).
    const combos = within(dialog).getAllByRole('combobox');
    expect(combos.some((c) => c.textContent?.includes('HTTP'))).toBe(true);
    // The HTTP variant contributes Base URL + Headers.
    expect(within(dialog).getByText('Base URL')).toBeDefined();
    expect(within(dialog).getByText('Headers')).toBeDefined();
  });
});

describe('ConnectorsAdmin — auto-derived identifier', () => {
  it('derives the identifier slug from the display name', () => {
    const dialog = renderCreate();
    const displayName = within(dialog).getAllByRole('textbox')[0];
    fireEvent.change(displayName, { target: { value: 'Stripe Payments' } });
    expect(within(dialog).getByText('stripe-payments')).toBeDefined();
  });
});

describe('ConnectorsAdmin — table', () => {
  it('describes connectors without implying HTTP-only', () => {
    renderTable({ connectors: [connector] });
    const desc = screen.getByText(
      /Reusable, credentialed connections to external systems/,
    );
    expect(desc.textContent).not.toContain('HTTP');
    expect(desc.textContent).not.toContain('endpoint');
  });

  it('renders generic empty copy while keeping the sortable headers mounted', () => {
    render(
      <ConnectorsAdmin.Provider value={makeValue({ connectors: [] })}>
        <ConnectorsAdmin.Root />
      </ConnectorsAdmin.Provider>,
    );
    expect(screen.getByText('No connectors yet.')).toBeDefined();
    // The header must not unmount on the empty state.
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('keeps the header and filter row mounted while loading', () => {
    render(
      <ConnectorsAdmin.Provider value={makeValue({ connectors: [], isLoading: true })}>
        <ConnectorsAdmin.Root />
      </ConnectorsAdmin.Provider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    expect(screen.getByText('Loading connectors…')).toBeDefined();
    // Header + filter input survive the loading state (no focus-dropping unmount).
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
    expect(screen.getByRole('searchbox')).toBeDefined();
  });

  it('renders the connector config type as a badge', () => {
    renderTable({ connectors: [connector] });
    expect(screen.getByRole('columnheader', { name: 'Type' })).toBeDefined();
    expect(screen.getByText('HTTP')).toBeDefined();
  });

  it('shows "Cloud" for a connector with no agent', () => {
    renderTable({ connectors: [connector] });
    expect(screen.getByText('Cloud')).toBeDefined();
  });

  it('resolves an agent resource name to its display label', () => {
    const agent = 'organizations/acme/storageGateways/gw1/agents/a1';
    renderTable({
      connectors: [{ ...connector, agent }],
      agentOptions: [{ value: agent, label: 'edge-01' }],
    });
    expect(screen.getByText('edge-01')).toBeDefined();
  });

  it('leaves the Space column blank for an org-direct connector', () => {
    renderTable({ connectors: [connector] });
    expect(screen.getByRole('columnheader', { name: 'Space' })).toBeDefined();
    // The column cell is blank for org-direct rows (form still labels it).
    expect(screen.queryByText('Organization')).toBeNull();
  });

  it('resolves a space-scoped connector name to its space label', () => {
    renderTable({ connectors: [spaceConnector], spaceOptions });
    expect(screen.getByText('Main')).toBeDefined();
  });

  it('falls back to the space slug when the space is unresolved', () => {
    renderTable({ connectors: [spaceConnector] });
    expect(screen.getByText('main')).toBeDefined();
  });

  it('hides the Space column inside a specific space (redundant there)', () => {
    // Org rollup (scope '') shows Space; a specific-space scope drops the column
    // since every row shares that space.
    renderTable({ connectors: [spaceConnector], spaceOptions, scope: 'main' });
    expect(screen.queryByRole('columnheader', { name: 'Space' })).toBeNull();
  });

  it('opens the edit dialog when the name link is clicked', () => {
    const openEdit = vi.fn();
    renderTable({ connectors: [connector] }, { openEdit });
    fireEvent.click(screen.getByRole('button', { name: 'Stripe' }));
    expect(openEdit).toHaveBeenCalledWith(connector);
  });

  it('renders edit/delete as icon actions, with destructive styling on delete', () => {
    const openEdit = vi.fn();
    const openRemove = vi.fn();
    renderTable({ connectors: [connector] }, { openEdit, openRemove });

    const edit = screen.getByRole('button', { name: 'Edit connector' });
    const remove = screen.getByRole('button', { name: 'Delete connector' });
    // Icon buttons carry no text label.
    expect(edit.textContent).toBe('');
    expect(remove.textContent).toBe('');
    // Delete is styled destructive.
    expect(remove.className).toContain('text-destructive');

    fireEvent.click(edit);
    expect(openEdit).toHaveBeenCalledWith(connector);
    fireEvent.click(remove);
    expect(openRemove).toHaveBeenCalledWith(connector);
  });
});

describe('ConnectorsAdmin — list controls', () => {
  it('toggles the filter row and reflects the on/off state', () => {
    renderTable({ connectors: [connector] });
    const button = screen.getByRole('button', { name: 'Filter' });
    // Hidden and off until pressed.
    expect(screen.queryByRole('searchbox')).toBeNull();
    expect(button.getAttribute('aria-pressed')).toBe('false');

    fireEvent.click(button);
    expect(screen.getByRole('searchbox')).toBeDefined();
    expect(button.getAttribute('aria-pressed')).toBe('true');

    fireEvent.click(button);
    expect(screen.queryByRole('searchbox')).toBeNull();
    expect(button.getAttribute('aria-pressed')).toBe('false');
  });

  it('drives the name filter through setFilter (debounced)', () => {
    vi.useFakeTimers();
    try {
      const setFilter = vi.fn();
      renderTable({ connectors: [connector] }, { setFilter });
      fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
      fireEvent.change(screen.getByRole('searchbox'), {
        target: { value: 'stripe' },
      });
      // Debounced: no commit until the window elapses.
      expect(setFilter).not.toHaveBeenCalled();
      act(() => vi.advanceTimersByTime(300));
      // Debounced text commits with 'replace' history.
      expect(setFilter).toHaveBeenCalledWith('displayName', 'stripe', 'replace');
    } finally {
      vi.useRealTimers();
    }
  });

  it('renders sortable Name and Updated headers wired to toggleSort', () => {
    const toggleSort = vi.fn();
    renderTable({ connectors: [connector] }, { toggleSort });

    fireEvent.click(screen.getByRole('button', { name: 'Name' }));
    expect(toggleSort).toHaveBeenCalledWith('displayName');

    fireEvent.click(screen.getByRole('button', { name: 'Updated' }));
    expect(toggleSort).toHaveBeenCalledWith('updateTime');
  });

  it('marks the active sort column via aria-sort', () => {
    renderTable({
      connectors: [connector],
      sort: { field: 'updateTime', direction: 'desc' },
    });
    expect(
      screen.getByRole('columnheader', { name: 'Updated' }).getAttribute('aria-sort'),
    ).toBe('descending');
    expect(
      screen.getByRole('columnheader', { name: 'Name' }).getAttribute('aria-sort'),
    ).toBe('none');
  });

  it('distinguishes a filtered-empty result while keeping controls mounted', () => {
    renderTable({ connectors: [], filters: { displayName: 'zzz' } });
    expect(screen.getByText('No connectors match your filters.')).toBeDefined();
    // The header stays so the filter can still be cleared with no rows.
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('shows Clear filters only when a filter or scope is active', () => {
    const clearFilters = vi.fn();
    // Clean: no clear affordance.
    const { unmount } = render(
      <ConnectorsAdmin.Provider value={makeValue({ connectors: [connector] })}>
        <ConnectorsAdmin.Root />
      </ConnectorsAdmin.Provider>,
    );
    expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull();
    unmount();

    // Active filter: clear affordance appears and fires clearFilters.
    renderTable(
      { connectors: [connector], filters: { displayName: 'stripe' } },
      { clearFilters },
    );
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(clearFilters).toHaveBeenCalled();
  });

  it('shows Clear filters when a non-default scope is active', () => {
    renderTable({ connectors: [connector], scope: 'main' });
    expect(screen.getByRole('button', { name: 'Clear filters' })).toBeDefined();
  });

  it('pages forward and back through the cursor pager', () => {
    const nextPage = vi.fn();
    const prevPage = vi.fn();
    renderTable(
      {
        connectors: [connector],
        pagination: { hasPrevPage: true, hasNextPage: true },
      },
      { nextPage, prevPage },
    );
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(nextPage).toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
    expect(prevPage).toHaveBeenCalled();
  });

  it('disables the pager buttons when a single page fits', () => {
    renderTable({ connectors: [connector] });
    expect(
      screen.getByRole('button', { name: 'Next' }).hasAttribute('disabled'),
    ).toBe(true);
    expect(
      screen.getByRole('button', { name: 'Previous' }).hasAttribute('disabled'),
    ).toBe(true);
  });
});

describe('ConnectorsAdmin — scope', () => {
  it('shows the scope combobox (resting on "All spaces") as a gated toolbar control', () => {
    renderTable({ connectors: [connector], spaceOptions });
    // Scope is a toolbar control the connectors consumer wires (the grid knows
    // nothing about scope); it's gated by the same filter toggle as the row.
    expect(screen.queryByPlaceholderText('All spaces')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    // The combobox input rests on the "All spaces" placeholder when empty.
    expect(screen.getByPlaceholderText('All spaces')).toBeDefined();
  });

  it('shows an unset placeholder for org-direct scope in the create form', () => {
    const dialog = renderCreate();
    expect(within(dialog).getByText('Scope')).toBeDefined();
    // Org-direct (scope === '') reads as unset — the combobox rests on its
    // placeholder, not a selected "Organization" value.
    expect(
      within(dialog).getByPlaceholderText('No space — organization'),
    ).toBeDefined();
  });

  it('shows the scope read-only (not a control) when editing', () => {
    const dialog = renderEdit(spaceConnector);
    // The space label renders in a disabled input, not an editable select.
    const scope = within(dialog).getByDisplayValue('Main');
    expect(scope.hasAttribute('disabled')).toBe(true);
  });
});

describe('ConnectorsAdmin — agent filter facet', () => {
  const agent = 'organizations/acme/storageGateways/gw/agents/a1';

  it('hides the agent filter when no agents are in scope', () => {
    renderTable({ connectors: [connector], agentsInUse: [] });
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    // The Name + Scope filters render; the agent facet does not.
    expect(screen.getByRole('searchbox')).toBeDefined();
    expect(screen.queryByPlaceholderText('Any agent')).toBeNull();
  });

  it('shows the agent filter when agents are in scope', () => {
    renderTable({
      connectors: [connector],
      agentsInUse: [agent],
      agentOptions: [{ value: agent, label: 'edge-01' }],
    });
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    // The agent combobox rests on the "Any agent" placeholder.
    expect(screen.getByPlaceholderText('Any agent')).toBeDefined();
  });
});

describe('ConnectorsAdmin — Run on Agent dropdown', () => {
  const createDialog = {
    open: true,
    mode: 'create' as const,
    editing: null,
    error: null,
    pending: false,
  };

  it('relabels the field to "Run on Agent" and defaults to none in the form', () => {
    render(
      <ConnectorsAdmin.Provider
        value={makeValue({
          connectors: [],
          agentOptions: [
            {
              value: 'organizations/acme/storageGateways/gw1/agents/a1',
              label: 'edge-01',
            },
          ],
          dialog: createDialog,
        })}
      >
        <ConnectorsAdmin.Root />
      </ConnectorsAdmin.Provider>,
    );
    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getByText('Run on Agent')).toBeDefined();
    // The agent combobox rests on the "None (runs in cloud)" placeholder.
    expect(
      within(dialog).getByPlaceholderText('None (runs in cloud)'),
    ).toBeDefined();
  });
});
