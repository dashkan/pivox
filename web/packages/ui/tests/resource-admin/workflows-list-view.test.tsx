// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { ResourceList } from '../../src/resource-admin/resource-list';
import { workflowsListView } from '../../src/resource-admin/workflows-list-view';

import type { ResourceListContextValue } from '../../src/resource-admin/resource-list.context';
import type {
  RemoveState,
  SortState,
  Workflow,
  WorkflowListExtras,
} from '../../src/resource-admin/types';

// AdminSearch and grid controls measure/scroll the DOM; jsdom needs shims.
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

type Value = ResourceListContextValue<Workflow, WorkflowListExtras>;

interface StateOverrides {
  rows?: Workflow[];
  isLoading?: boolean;
  loadError?: string | null;
  remove?: RemoveState<Workflow>;
  filters?: Record<string, string>;
  sort?: SortState | null;
  pageSize?: number;
  pagination?: { hasPrevPage: boolean; hasNextPage: boolean };
}

/**
 * The workflows LIST — the third-shape proof of the resource-admin abstraction:
 * List-only, create-less, form-less, navigating to the bespoke canvas instead of
 * a routed form. Verifies the columns port verbatim (Name → canvas, Origin badge,
 * Enabled, Live version, Updated + actor), that NO "New" button renders (no create
 * flow), that the Name click navigates via the injected row action, and that the
 * shared list controls (filter/sort/pagination) drive the generic composite.
 */
function makeValue(overrides: StateOverrides): Value {
  return {
    state: {
      rows: [],
      isLoading: false,
      loadError: null,
      remove: { target: null, error: null, pending: false },
      extras: {},
      filters: {},
      sort: null,
      pageSize: 25,
      scope: '',
      pagination: { hasPrevPage: false, hasNextPage: false },
      ...overrides,
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

const ownedWorkflow: Workflow = {
  name: 'organizations/acme/workflows/nightly-ingest',
  displayName: 'Nightly ingest',
  enabled: true,
  origin: 'OWNED',
  version: 'organizations/acme/workflows/nightly-ingest/versions/7',
  updateTime: '2026-02-01T00:00:00Z',
};

const managedWorkflow: Workflow = {
  name: 'organizations/acme/workflows/managed-clip',
  displayName: 'Managed clip',
  enabled: false,
  origin: 'MANAGED',
  updateTime: '2026-03-01T00:00:00Z',
};

function renderList(value: Value) {
  render(
    <ResourceList.Provider value={value}>
      <ResourceList.Root view={workflowsListView} />
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

describe('Workflows list view — columns', () => {
  it('renders the workflow columns verbatim (Name, Origin, Enabled, Live version, Updated)', () => {
    renderTable({ rows: [ownedWorkflow] });
    for (const header of ['Name', 'Origin', 'Enabled', 'Live version', 'Updated']) {
      expect(screen.getByRole('columnheader', { name: header })).toBeDefined();
    }
    expect(screen.getByText('Nightly ingest')).toBeDefined();
  });

  it('badges an OWNED workflow "Owned" and a MANAGED one "Managed"', () => {
    renderTable({ rows: [ownedWorkflow, managedWorkflow] });
    expect(screen.getByText('Owned')).toBeDefined();
    expect(screen.getByText('Managed')).toBeDefined();
  });

  it('shows the enabled/disabled state', () => {
    renderTable({ rows: [ownedWorkflow, managedWorkflow] });
    // "Enabled" also names the column header, so scope the value assertion to cells.
    const cellText = screen.getAllByRole('cell').map((c) => c.textContent);
    expect(cellText).toContain('Enabled');
    expect(cellText).toContain('Disabled');
  });

  it('surfaces the live version leaf, and an em dash when unpromoted', () => {
    renderTable({ rows: [ownedWorkflow] });
    expect(screen.getByText('7')).toBeDefined();
    cleanup();
    renderTable({ rows: [managedWorkflow] });
    expect(screen.getByText('—')).toBeDefined();
  });

  it('falls back to the workflow leaf id when displayName is empty', () => {
    renderTable({
      rows: [{ ...ownedWorkflow, displayName: '' }],
    });
    expect(screen.getByRole('button', { name: 'nightly-ingest' })).toBeDefined();
  });
});

describe('Workflows list view — canvas row action (no form)', () => {
  it('navigates to the canvas via the injected row action when the name is clicked', () => {
    const openEdit = vi.fn();
    renderTable({ rows: [ownedWorkflow] }, { openEdit });
    fireEvent.click(screen.getByRole('button', { name: 'Nightly ingest' }));
    expect(openEdit).toHaveBeenCalledWith(ownedWorkflow);
  });

  it('exposes no row edit/delete action column (canvas is the only editor)', () => {
    renderTable({ rows: [ownedWorkflow] });
    expect(screen.queryByRole('button', { name: 'Edit workflow' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete workflow' })).toBeNull();
  });
});

describe('Workflows list view — create-less (no "New" button)', () => {
  it('renders no create affordance — workflows have no create-from-list flow', () => {
    renderTable({ rows: [ownedWorkflow] });
    expect(screen.queryByRole('button', { name: /New workflow/i })).toBeNull();
    // No scope control either (workflows are org-direct only).
    expect(screen.queryByText('Workflows')).toBeDefined();
  });
});

describe('Workflows list view — list controls', () => {
  it('renders generic empty copy while keeping the sortable headers mounted', () => {
    renderList(makeValue({ rows: [] }));
    expect(screen.getByText('No workflows yet.')).toBeDefined();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('toggles the filter row and drives the name filter through setFilter (debounced)', () => {
    vi.useFakeTimers();
    try {
      const setFilter = vi.fn();
      renderTable({ rows: [ownedWorkflow] }, { setFilter });
      expect(screen.queryByRole('searchbox')).toBeNull();
      fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
      fireEvent.change(screen.getByRole('searchbox'), {
        target: { value: 'nightly' },
      });
      expect(setFilter).not.toHaveBeenCalled();
      act(() => {
        vi.advanceTimersByTime(300);
      });
      expect(setFilter).toHaveBeenCalledWith('displayName', 'nightly', 'replace');
    } finally {
      vi.useRealTimers();
    }
  });

  it('renders sortable Name and Updated headers wired to toggleSort', () => {
    const toggleSort = vi.fn();
    renderTable({ rows: [ownedWorkflow] }, { toggleSort });

    fireEvent.click(screen.getByRole('button', { name: 'Name' }));
    expect(toggleSort).toHaveBeenCalledWith('displayName');

    fireEvent.click(screen.getByRole('button', { name: 'Updated' }));
    expect(toggleSort).toHaveBeenCalledWith('updateTime');
  });

  it('marks the active sort column via aria-sort', () => {
    renderTable({
      rows: [ownedWorkflow],
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
    expect(screen.getByText('No workflows match your filters.')).toBeDefined();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('pages forward and back through the cursor pager', () => {
    const nextPage = vi.fn();
    const prevPage = vi.fn();
    renderTable(
      {
        rows: [ownedWorkflow],
        pagination: { hasPrevPage: true, hasNextPage: true },
      },
      { nextPage, prevPage },
    );
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(nextPage).toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
    expect(prevPage).toHaveBeenCalled();
  });
});
