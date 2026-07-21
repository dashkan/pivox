// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { actionsColumn } from '../../src/resource-admin/actions-column';
import { ResourceList } from '../../src/resource-admin/resource-list';

import type {
  ResourceColumnContext,
  ResourceListContextValue,
  ResourceListView,
} from '../../src/resource-admin/resource-list.context';

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

interface Row {
  id: string;
  displayName: string;
}

type Value = ResourceListContextValue<Row, Record<string, never>>;

const row: Row = { id: 'r1', displayName: 'Row one' };

/**
 * Composition proof for the affordance layer: `ResourceList.Default` composes the
 * New button (presence = create) + the edit+delete actions column + the confirm
 * dialog; a bare `ResourceList.Root` composes none of them. The create/delete
 * coupling is dissolved — the affordances exist because they are composed in, not
 * because a `newLabel`/`deleteConfirm` descriptor flag turned them on.
 */
function makeValue(overrides?: {
  rows?: Row[];
  remove?: Value['state']['remove'];
}): Value {
  return {
    state: {
      rows: overrides?.rows ?? [row],
      isLoading: false,
      loadError: null,
      remove: overrides?.remove ?? { target: null, error: null, pending: false },
      extras: {},
      filters: {},
      sort: null,
      pageSize: 25,
      scope: '',
      pagination: { hasPrevPage: false, hasNextPage: false },
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

const view: ResourceListView<Row, Record<string, never>> = {
  title: 'Rows',
  description: 'A generic list.',
  loadingLabel: 'Loading rows…',
  emptyLabel: (active) => (active ? 'No rows match.' : 'No rows yet.'),
  hasActiveFilters: (filters) => Boolean(filters.displayName?.trim()),
  rowKey: (r) => r.id,
  columns: (ctx) => [
    {
      field: 'displayName',
      header: 'Name',
      sortable: true,
      cell: (r) => (
        <button type="button" onClick={() => ctx.onEdit(r)}>
          {r.displayName}
        </button>
      ),
    },
  ],
};

const confirmDelete = (r: Row) => `Permanently delete "${r.displayName}".`;

function renderDefault(value: Value) {
  render(
    <ResourceList.Provider value={value}>
      <ResourceList.Default
        view={view}
        noun="row"
        confirmDelete={confirmDelete}
      />
    </ResourceList.Provider>,
  );
}

function renderBare(value: Value) {
  render(
    <ResourceList.Provider value={value}>
      <ResourceList.Root view={view} />
    </ResourceList.Provider>,
  );
}

describe('ResourceList.Default — composed create + edit/delete', () => {
  it('renders the New button (presence = create) wired to openCreate', () => {
    const value = makeValue();
    const openCreate = vi.fn();
    value.actions.openCreate = openCreate;
    renderDefault(value);
    fireEvent.click(screen.getByRole('button', { name: 'New row' }));
    expect(openCreate).toHaveBeenCalledTimes(1);
  });

  it('renders an edit + delete actions column wired to onEdit / openRemove', () => {
    const value = makeValue();
    const openEdit = vi.fn();
    const openRemove = vi.fn();
    value.actions.openEdit = openEdit;
    value.actions.openRemove = openRemove;
    renderDefault(value);

    const edit = screen.getByRole('button', { name: 'Edit row' });
    const remove = screen.getByRole('button', { name: 'Delete row' });
    // Icon buttons carry no text; delete is destructive.
    expect(edit.textContent).toBe('');
    expect(remove.textContent).toBe('');
    expect(remove.className).toContain('text-destructive');

    fireEvent.click(edit);
    expect(openEdit).toHaveBeenCalledWith(row);
    fireEvent.click(remove);
    expect(openRemove).toHaveBeenCalledWith(row);
  });

  it('drives the confirm dialog copy from the composed confirmDelete param', () => {
    renderDefault(
      makeValue({ remove: { target: row, error: null, pending: false } }),
    );
    expect(screen.getByText('Delete row?')).toBeDefined();
    expect(screen.getByText('Permanently delete "Row one".')).toBeDefined();
  });
});

describe('ResourceList.Root — bare (no composed affordances)', () => {
  it('renders no New button, no actions column, and no confirm dialog', () => {
    renderBare(
      makeValue({ remove: { target: row, error: null, pending: false } }),
    );
    expect(screen.queryByRole('button', { name: 'New row' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Edit row' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete row' })).toBeNull();
    // Even with a remove target set, no dialog renders without a composed delete.
    expect(screen.queryByText('Delete row?')).toBeNull();
    // The list itself still renders.
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });
});

describe('actionsColumn factory — presence-driven edit/delete', () => {
  const ctx: ResourceColumnContext<Row, Record<string, never>> = {
    scope: '',
    showFilters: false,
    extras: {},
    onEdit: noop,
    openRemove: noop,
  };

  it('renders edit-only when no delete opt is supplied', () => {
    const col = actionsColumn(ctx, { edit: true, editLabel: 'Edit row', removeLabel: 'Delete row' });
    render(<>{col.cell(row)}</>);
    expect(screen.getByRole('button', { name: 'Edit row' })).toBeDefined();
    expect(screen.queryByRole('button', { name: 'Delete row' })).toBeNull();
  });
});
