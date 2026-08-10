'use client';

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@pivox/primitives/table';

import { CursorPager, PageSizeSelect } from './cursor-pagination';
import { GridContext, useGrid } from './grid.context';
import { SortableHeader } from './sortable-header';

import type { GridColumn, GridContextValue } from './types';
import type { ReactNode } from 'react';

/**
 * Generic, dumb, controlled data grid — a compound component
 * (architecture-compound-components). It fetches nothing, owns no list state,
 * and knows no domain, router, or SSR. A `Grid.Provider` injects the
 * `{ state, actions, meta }` interface (state-context-interface); every part
 * reads it via `useGrid()`. Consumers compose exactly the parts they want — no
 * boolean toggles (architecture-avoid-boolean-props).
 *
 * ```tsx
 * <SomeProvider>            // maps domain state → GridContextValue<T>
 *   <Grid.Toolbar>…</Grid.Toolbar>
 *   <Grid.Table columns={cols} emptyLabel="…" />
 *   <Grid.CursorPagination />
 * </SomeProvider>
 * ```
 */

/** DI boundary: the only place that knows how state is produced. */
function GridProvider<T>({
  value,
  children,
}: {
  value: GridContextValue<T>;
  children: ReactNode;
}) {
  // eslint-disable-next-line typescript/no-unsafe-type-assertion -- widen the consumer's typed value to the unknown-rowed context (React context is invariant); useGrid<T> re-narrows. The DI boundary needs this cast.
  const injected = value as GridContextValue<unknown>;
  return <GridContext value={injected}>{children}</GridContext>;
}

/** A composition slot rendered above the table (filter toggle, scope, clear, …). */
function GridToolbar({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-2">{children}</div>;
}

/** Full-width body row for loading / error / empty, so the header stays mounted. */
function GridNoticeRow({
  colSpan,
  children,
}: {
  colSpan: number;
  children: ReactNode;
}) {
  return (
    <TableRow>
      <TableCell
        colSpan={colSpan}
        className="h-24 text-center text-sm text-muted-foreground"
      >
        {children}
      </TableCell>
    </TableRow>
  );
}

/**
 * The table: renders from a typed `columns` array (not introspected children).
 * A header row (sortable where asked) that stays mounted across data states, an
 * optional column-aligned filter row (present when any column supplies a
 * `filter`), and a body that reflects loading → error → empty → data via a
 * ternary chain (rendering-conditional-render).
 */
function GridTable<T>({
  columns,
  emptyLabel,
  loadingLabel,
}: {
  columns: GridColumn<T>[];
  /** Body content when the load succeeds with zero rows. */
  emptyLabel?: ReactNode;
  /** Body content while loading. */
  loadingLabel?: ReactNode;
}) {
  const { state, actions, meta } = useGrid<T>();
  const colSpan = columns.length;
  const hasFilterRow = columns.some(
    (column) => column.filter !== undefined && column.filter !== null,
  );

  const body = state.isLoading ? (
    <GridNoticeRow colSpan={colSpan}>
      {loadingLabel ?? 'Loading…'}
    </GridNoticeRow>
  ) : state.loadError !== null ? (
    <GridNoticeRow colSpan={colSpan}>{state.loadError}</GridNoticeRow>
  ) : state.rows.length === 0 ? (
    <GridNoticeRow colSpan={colSpan}>
      {emptyLabel ?? 'No results.'}
    </GridNoticeRow>
  ) : (
    state.rows.map((row) => (
      <TableRow key={meta.rowKey(row)}>
        {columns.map((column, index) => (
          <TableCell
            key={column.field ?? `col-${index}`}
            className={column.cellClassName}
          >
            {column.cell(row)}
          </TableCell>
        ))}
      </TableRow>
    ))
  );

  return (
    <Table>
      <TableHeader>
        <TableRow>
          {columns.map((column, index) =>
            column.sortable && column.field !== undefined ? (
              <SortableHeader
                key={column.field}
                field={column.field}
                sort={state.sort}
                onToggle={actions.toggleSort}
                className={column.className}
              >
                {column.header}
              </SortableHeader>
            ) : (
              <TableHead
                key={column.field ?? `col-${index}`}
                className={column.className}
              >
                {column.header}
              </TableHead>
            ),
          )}
        </TableRow>
        {hasFilterRow ? (
          <TableRow>
            {columns.map((column, index) => (
              <TableHead key={column.field ?? `col-${index}`}>
                {column.filter ?? null}
              </TableHead>
            ))}
          </TableRow>
        ) : null}
      </TableHeader>
      <TableBody>{body}</TableBody>
    </Table>
  );
}

/**
 * Cursor pagination: page-size selector + Prev/Next pager, driven entirely by the
 * injected interface. An explicit variant (patterns-explicit-variants) — a
 * numbered pager would be a separate `Grid.NumberedPagination` sibling, not a
 * `mode` prop on this one.
 */
function GridCursorPagination() {
  const { state, actions } = useGrid<unknown>();
  return (
    <div className="flex items-center justify-end gap-4">
      <PageSizeSelect
        pageSize={state.pageSize}
        onPageSizeChange={actions.setPageSize}
      />
      <CursorPager
        hasPrev={state.pagination.hasPrev}
        hasNext={state.pagination.hasNext}
        onPrev={actions.prevPage}
        onNext={actions.nextPage}
      />
    </div>
  );
}

/** The compound grid. Consumers compose the parts they want. */
export const Grid = {
  Provider: GridProvider,
  Toolbar: GridToolbar,
  Table: GridTable,
  CursorPagination: GridCursorPagination,
};
