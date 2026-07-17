// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import { Grid } from '../../src/grid/grid';
import { useGrid } from '../../src/grid/grid.context';

import type { GridColumn, GridContextValue } from '../../src/grid/types';
import type { ReactNode } from 'react';

// CursorPagination uses a Radix Select which measures/scrolls the DOM.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

// A generic, non-domain row — the grid must not know anything about it beyond
// the injected interface.
interface Row {
  id: string;
  name: string;
  size: number;
}

const rows: Row[] = [
  { id: 'a', name: 'Alpha', size: 10 },
  { id: 'b', name: 'Beta', size: 20 },
];

const noop = (): void => {};

function makeValue(
  state: Partial<GridContextValue<Row>['state']> = {},
  actions: Partial<GridContextValue<Row>['actions']> = {},
): GridContextValue<Row> {
  return {
    state: {
      rows,
      isLoading: false,
      loadError: null,
      filters: {},
      sort: null,
      pageSize: 25,
      pagination: { hasPrev: false, hasNext: false },
      ...state,
    },
    actions: {
      setFilter: noop,
      toggleSort: noop,
      setPageSize: noop,
      clearFilters: noop,
      nextPage: noop,
      prevPage: noop,
      ...actions,
    },
    meta: { rowKey: (row) => row.id },
  };
}

/** A filter control that reads the injected interface (the DI it demonstrates). */
function NameFilter() {
  const { state, actions } = useGrid<Row>();
  return (
    <input
      aria-label="name-filter"
      value={state.filters.name ?? ''}
      onChange={(event) => actions.setFilter('name', event.target.value)}
    />
  );
}

/** The composed UI under test — the same parts a real consumer would compose. */
function TestGrid({
  value,
  filter,
}: {
  value: GridContextValue<Row>;
  filter?: ReactNode;
}) {
  const columns: GridColumn<Row>[] = [
    {
      field: 'name',
      header: 'Name',
      sortable: true,
      filter,
      cell: (row) => <span>{row.name}</span>,
    },
    { header: 'Size', cellClassName: 'muted', cell: (row) => row.size },
  ];
  return (
    <Grid.Provider value={value}>
      <Grid.Table
        columns={columns}
        emptyLabel="Nothing here"
        loadingLabel="Loading rows…"
      />
      <Grid.CursorPagination />
    </Grid.Provider>
  );
}

describe('Grid — columns array', () => {
  it('renders one header per column in the array', () => {
    render(<TestGrid value={makeValue()} />);
    expect(screen.getAllByRole('columnheader')).toHaveLength(2);
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
    expect(screen.getByRole('columnheader', { name: 'Size' })).toBeDefined();
  });

  it('renders a body cell per row via each column cell fn', () => {
    render(<TestGrid value={makeValue()} />);
    expect(screen.getByText('Alpha')).toBeDefined();
    expect(screen.getByText('Beta')).toBeDefined();
    expect(screen.getByText('10')).toBeDefined();
    expect(screen.getByText('20')).toBeDefined();
  });
});

describe('Grid — sorting', () => {
  it('renders a sortable header wired to toggleSort via context', () => {
    const toggleSort = vi.fn();
    render(<TestGrid value={makeValue({}, { toggleSort })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Name' }));
    expect(toggleSort).toHaveBeenCalledWith('name');
  });

  it('marks the active sort column via aria-sort', () => {
    render(
      <TestGrid value={makeValue({ sort: { field: 'name', direction: 'asc' } })} />,
    );
    expect(
      screen.getByRole('columnheader', { name: 'Name' }).getAttribute('aria-sort'),
    ).toBe('ascending');
    // A non-sortable column carries no aria-sort at all (only sortable headers do).
    expect(
      screen.getByRole('columnheader', { name: 'Size' }).getAttribute('aria-sort'),
    ).toBeNull();
  });

  it('does not make a non-sortable column a button', () => {
    render(<TestGrid value={makeValue()} />);
    expect(screen.queryByRole('button', { name: 'Size' })).toBeNull();
  });
});

describe('Grid — filter row', () => {
  it('omits the filter row when no column supplies a filter', () => {
    render(<TestGrid value={makeValue()} />);
    expect(screen.queryByLabelText('name-filter')).toBeNull();
  });

  it('renders the filter row when a column supplies a filter control', () => {
    render(<TestGrid value={makeValue()} filter={<NameFilter />} />);
    expect(screen.getByLabelText('name-filter')).toBeDefined();
  });

  it('drives the injected setFilter from the column filter control', () => {
    const setFilter = vi.fn();
    render(
      <TestGrid value={makeValue({}, { setFilter })} filter={<NameFilter />} />,
    );
    fireEvent.change(screen.getByLabelText('name-filter'), {
      target: { value: 'alp' },
    });
    expect(setFilter).toHaveBeenCalledWith('name', 'alp');
  });
});

describe('Grid — body states', () => {
  it('shows the loading label while loading, header still mounted', () => {
    render(<TestGrid value={makeValue({ isLoading: true, rows: [] })} />);
    expect(screen.getByText('Loading rows…')).toBeDefined();
    // The header must not unmount on a data-state swap (focus preservation).
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeDefined();
  });

  it('shows the load-error message on error', () => {
    render(<TestGrid value={makeValue({ loadError: 'Boom', rows: [] })} />);
    expect(screen.getByText('Boom')).toBeDefined();
    expect(screen.queryByText('Nothing here')).toBeNull();
  });

  it('shows the empty label on a zero-row success', () => {
    render(<TestGrid value={makeValue({ rows: [] })} />);
    expect(screen.getByText('Nothing here')).toBeDefined();
  });

  it('prefers loading over empty when both could apply', () => {
    render(<TestGrid value={makeValue({ isLoading: true, rows: [] })} />);
    expect(screen.getByText('Loading rows…')).toBeDefined();
    expect(screen.queryByText('Nothing here')).toBeNull();
  });
});

describe('Grid — cursor pagination', () => {
  it('reads pageSize + pagination flags from context', () => {
    render(
      <TestGrid
        value={makeValue({
          pageSize: 25,
          pagination: { hasPrev: true, hasNext: true },
        })}
      />,
    );
    expect(
      screen.getByRole('button', { name: 'Previous' }).hasAttribute('disabled'),
    ).toBe(false);
    expect(
      screen.getByRole('button', { name: 'Next' }).hasAttribute('disabled'),
    ).toBe(false);
  });

  it('disables the pager when no adjacent page exists', () => {
    render(<TestGrid value={makeValue()} />);
    expect(
      screen.getByRole('button', { name: 'Previous' }).hasAttribute('disabled'),
    ).toBe(true);
    expect(
      screen.getByRole('button', { name: 'Next' }).hasAttribute('disabled'),
    ).toBe(true);
  });

  it('fires nextPage / prevPage through the injected actions', () => {
    const nextPage = vi.fn();
    const prevPage = vi.fn();
    render(
      <TestGrid
        value={makeValue(
          { pagination: { hasPrev: true, hasNext: true } },
          { nextPage, prevPage },
        )}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(nextPage).toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
    expect(prevPage).toHaveBeenCalled();
  });
});

describe('Grid — dependency injection', () => {
  it('throws when a grid part is used outside a provider', () => {
    // Silence the expected React error boundary log.
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() =>
      render(<Grid.Table<Row> columns={[]} />),
    ).toThrow(/within a <Grid.Provider>/);
    spy.mockRestore();
  });
});
