// @vitest-environment jsdom
import { act, fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import {
  connectorDeleteDescription,
  connectorsListView,
} from '../../src/resource-admin/connectors-list-view';
import { ResourceList } from '../../src/resource-admin/resource-list';

import type { ResourceListContextValue } from '../../src/resource-admin/resource-list.context';
import type {
  Connector,
  ConnectorListExtras,
  RemoveState,
  SortState,
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

type Value = ResourceListContextValue<Connector, ConnectorListExtras>;

interface StateOverrides {
  rows?: Connector[];
  isLoading?: boolean;
  loadError?: string | null;
  remove?: RemoveState<Connector>;
  extras?: Partial<ConnectorListExtras>;
  filters?: Record<string, string>;
  sort?: SortState | null;
  pageSize?: number;
  scope?: string;
  pagination?: { hasPrevPage: boolean; hasNextPage: boolean };
}

/**
 * The connectors LIST — now the generic `ResourceList` driven by the
 * `connectorsListView` descriptor + a DI'd resource-list value. This is the DOM
 * proof of the byte-identical migration off the old hand-written `ConnectorsAdmin`
 * bridge: the same rows, columns, scope/agent facets, sort, pagination, and quick
 * row-delete behaviors, verified against the generic composite.
 */
function makeValue(overrides: StateOverrides): Value {
  const { extras: extrasOverride, ...stateOverride } = overrides;
  return {
    state: {
      rows: [],
      isLoading: false,
      loadError: null,
      remove: { target: null, error: null, pending: false },
      extras: {
        agentOptions: [],
        agentsInUse: [],
        spaceOptions: [],
        ...extrasOverride,
      },
      filters: {},
      sort: null,
      pageSize: 25,
      scope: '',
      pagination: { hasPrevPage: false, hasNextPage: false },
      ...stateOverride,
    },
    actions: {
      openCreate: noop,
      openEdit: noop,
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

// Connectors compose the 90% preset: New button + edit+delete affordance column +
// confirm dialog. This is the composed-affordance twin of the old newLabel/
// deleteConfirm descriptor flags.
function renderList(value: Value) {
  render(
    <ResourceList.Provider value={value}>
      <ResourceList.Default
        view={connectorsListView}
        noun="connector"
        confirmDelete={connectorDeleteDescription}
      />
    </ResourceList.Provider>,
  );
}

function renderTable(
  overrides: StateOverrides,
  actions?: Partial<Value['actions']>,
): Value {
  const value = makeValue(overrides);
  if (actions) Object.assign(value.actions, actions);
  renderList(value);
  return value;
}

describe('Connectors list view — table', () => {
  it('describes connectors without implying HTTP-only', () => {
    renderTable({ rows: [connector] });
    const desc = screen.getByText(
      /Reusable, credentialed connections to external systems/,
    );
    expect(desc.textContent).not.toContain('HTTP');
    expect(desc.textContent).not.toContain('endpoint');
  });

  it('renders generic empty copy while keeping the sortable headers mounted', () => {
    renderList(makeValue({ rows: [] }));
    expect(screen.getByText('No connectors yet.')).toBeDefined();
    // The header must not unmount on the empty state.
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('keeps the header and filter row mounted while loading', () => {
    renderList(makeValue({ rows: [], isLoading: true }));
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    expect(screen.getByText('Loading connectors…')).toBeDefined();
    // Header + filter input survive the loading state (no focus-dropping unmount).
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
    expect(screen.getByRole('searchbox')).toBeDefined();
  });

  it('renders the connector config type as a badge', () => {
    renderTable({ rows: [connector] });
    expect(screen.getByRole('columnheader', { name: 'Type' })).toBeDefined();
    expect(screen.getByText('HTTP')).toBeDefined();
  });

  it('shows "Cloud" for a connector with no agent', () => {
    renderTable({ rows: [connector] });
    expect(screen.getByText('Cloud')).toBeDefined();
  });

  it('resolves an agent resource name to its display label', () => {
    const agent = 'organizations/acme/storageGateways/gw1/agents/a1';
    renderTable({
      rows: [{ ...connector, agent }],
      extras: { agentOptions: [{ value: agent, label: 'edge-01' }] },
    });
    expect(screen.getByText('edge-01')).toBeDefined();
  });

  it('leaves the Space column blank for an org-direct connector', () => {
    renderTable({ rows: [connector] });
    expect(screen.getByRole('columnheader', { name: 'Space' })).toBeDefined();
    // The column cell is blank for org-direct rows.
    expect(screen.queryByText('Organization')).toBeNull();
  });

  it('resolves a space-scoped connector name to its space label', () => {
    renderTable({ rows: [spaceConnector], extras: { spaceOptions } });
    expect(screen.getByText('Main')).toBeDefined();
  });

  it('falls back to the space slug when the space is unresolved', () => {
    renderTable({ rows: [spaceConnector] });
    expect(screen.getByText('main')).toBeDefined();
  });

  it('hides the Space column inside a specific space (redundant there)', () => {
    // Org rollup (scope '') shows Space; a specific-space scope drops the column
    // since every row shares that space.
    renderTable({
      rows: [spaceConnector],
      extras: { spaceOptions },
      scope: 'main',
    });
    expect(screen.queryByRole('columnheader', { name: 'Space' })).toBeNull();
  });

  it('navigates to the routed edit page when the name link is clicked', () => {
    const openEdit = vi.fn();
    renderTable({ rows: [connector] }, { openEdit });
    fireEvent.click(screen.getByRole('button', { name: 'Stripe' }));
    expect(openEdit).toHaveBeenCalledWith(connector);
  });

  it('renders edit/delete as icon actions, with destructive styling on delete', () => {
    const openEdit = vi.fn();
    const openRemove = vi.fn();
    renderTable({ rows: [connector] }, { openEdit, openRemove });

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

  it('navigates to the routed create page from the "New connector" button', () => {
    const openCreate = vi.fn();
    renderTable({ rows: [connector] }, { openCreate });
    fireEvent.click(screen.getByRole('button', { name: 'New connector' }));
    expect(openCreate).toHaveBeenCalledTimes(1);
  });
});

describe('Connectors list view — list controls', () => {
  it('toggles the filter row and reflects the on/off state', () => {
    renderTable({ rows: [connector] });
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
      renderTable({ rows: [connector] }, { setFilter });
      fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
      fireEvent.change(screen.getByRole('searchbox'), {
        target: { value: 'stripe' },
      });
      // Debounced: no commit until the window elapses.
      expect(setFilter).not.toHaveBeenCalled();
      act(() => {
        vi.advanceTimersByTime(300);
      });
      // Debounced text commits with 'replace' history.
      expect(setFilter).toHaveBeenCalledWith('displayName', 'stripe', 'replace');
    } finally {
      vi.useRealTimers();
    }
  });

  it('renders sortable Name and Updated headers wired to toggleSort', () => {
    const toggleSort = vi.fn();
    renderTable({ rows: [connector] }, { toggleSort });

    fireEvent.click(screen.getByRole('button', { name: 'Name' }));
    expect(toggleSort).toHaveBeenCalledWith('displayName');

    fireEvent.click(screen.getByRole('button', { name: 'Updated' }));
    expect(toggleSort).toHaveBeenCalledWith('updateTime');
  });

  it('marks the active sort column via aria-sort', () => {
    renderTable({
      rows: [connector],
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
    renderTable({ rows: [], filters: { displayName: 'zzz' } });
    expect(screen.getByText('No connectors match your filters.')).toBeDefined();
    // The header stays so the filter can still be cleared with no rows.
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('shows Clear filters only when a filter is active', () => {
    const clearFilters = vi.fn();
    // Clean: no clear affordance.
    const { unmount } = render(
      <ResourceList.Provider value={makeValue({ rows: [connector] })}>
        <ResourceList.Default
          view={connectorsListView}
          noun="connector"
          confirmDelete={connectorDeleteDescription}
        />
      </ResourceList.Provider>,
    );
    expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull();
    unmount();

    // Active filter: clear affordance appears and fires clearFilters.
    renderTable(
      { rows: [connector], filters: { displayName: 'stripe' } },
      { clearFilters },
    );
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(clearFilters).toHaveBeenCalled();
  });

  it('pages forward and back through the cursor pager', () => {
    const nextPage = vi.fn();
    const prevPage = vi.fn();
    renderTable(
      {
        rows: [connector],
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
    renderTable({ rows: [connector] });
    expect(
      screen.getByRole('button', { name: 'Next' }).hasAttribute('disabled'),
    ).toBe(true);
    expect(
      screen.getByRole('button', { name: 'Previous' }).hasAttribute('disabled'),
    ).toBe(true);
  });
});

describe('Connectors list view — agent filter facet', () => {
  const agent = 'organizations/acme/storageGateways/gw/agents/a1';

  it('hides the agent filter when no agents are in scope', () => {
    renderTable({ rows: [connector], extras: { agentsInUse: [] } });
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    // The Name filter renders; the agent facet does not.
    expect(screen.getByRole('searchbox')).toBeDefined();
    expect(screen.queryByPlaceholderText('Any agent')).toBeNull();
  });

  it('shows the agent filter when agents are in scope', () => {
    renderTable({
      rows: [connector],
      extras: {
        agentsInUse: [agent],
        agentOptions: [{ value: agent, label: 'edge-01' }],
      },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    // The agent combobox rests on the "Any agent" placeholder.
    expect(screen.getByPlaceholderText('Any agent')).toBeDefined();
  });
});
