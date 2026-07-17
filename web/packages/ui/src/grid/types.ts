import type { ReactNode } from 'react';

/** Sort direction for a column-driven, server-side order. */
export type SortDirection = 'asc' | 'desc';

/** The active column sort for a server-driven list. */
export interface SortState {
  /** The order_by field (e.g. `displayName`, `updateTime`). */
  field: string;
  direction: SortDirection;
}

/**
 * How a controls change should be recorded in history. Discrete changes
 * (page, sort, page size, clear) `push` a new entry so Back works; debounced
 * text (search) uses `replace` so keystrokes don't spam history. The grid is
 * router-agnostic — it only forwards this hint to the consumer's `onChange`.
 */
export type HistoryMode = 'push' | 'replace';

/**
 * Generic, controlled grid state — the read half of the DI interface. Fully
 * presentational: no cursor internals, no scope, no domain. `pagination` is
 * opaque (the grid can't tell cursor from offset); the consumer's provider maps
 * its own list state into this shape.
 */
export interface GridState<T> {
  rows: T[];
  isLoading: boolean;
  /** A load-error message to show in the body, or null. */
  loadError: string | null;
  /** Committed per-field filter values (e.g. `displayName`, `agent`). */
  filters: Record<string, string>;
  /** Active column sort, or null for the server default. */
  sort: SortState | null;
  /** Rows requested per page. Core state, shared by every pagination strategy. */
  pageSize: number;
  /**
   * The cursor-pagination slice, consumed by `Grid.CursorPagination`. A future
   * `Grid.NumberedPagination` would be an additive sibling reading its own slice
   * (e.g. `{ pageNum, pageCount }` + a `goToPage` action) — not a mode flag on
   * this one. `pageSize`/`setPageSize` stay core and shared across both.
   */
  pagination: { hasPrev: boolean; hasNext: boolean };
}

/** The write half of the DI interface — every grid mutation flows through here. */
export interface GridActions {
  /** Set (or clear, with '') one field's filter; `history` defaults to 'push'. */
  setFilter: (field: string, value: string, history?: HistoryMode) => void;
  /** Cycle a column's sort (unsorted → asc → desc → unsorted). */
  toggleSort: (field: string) => void;
  setPageSize: (size: number) => void;
  /** Reset all filters (the consumer decides whether that includes its own scope). */
  clearFilters: () => void;
  nextPage: () => void;
  prevPage: () => void;
}

/** Grid metadata the columns can't derive from a row alone. */
export interface GridMeta<T> {
  /** A stable React key for a row. */
  rowKey: (row: T) => string;
}

/**
 * The dependency-injected grid interface (state + actions + meta). Any provider
 * that implements this drives the same `Grid.*` UI — the connectors provider
 * today, a flat-resource or electron-local provider tomorrow.
 */
export interface GridContextValue<T> {
  state: GridState<T>;
  actions: GridActions;
  meta: GridMeta<T>;
}

/**
 * A declarative column descriptor. `Grid.Table` takes a typed `columns` array of
 * these — a plain data list, not introspected children. This is the sanctioned
 * "render-fn for per-row data" carve-out (patterns-children-over-render-props,
 * the `<List renderItem>` case): `cell` passes the row back, everything else is
 * static config. An array (vs child introspection) can't silently drop a column
 * wrapped in a Fragment/component, and lets a higher tier compose column sets.
 */
export interface GridColumn<T> {
  /** The order_by field; identifies a sortable column. Omit for display-only columns. */
  field?: string;
  header: ReactNode;
  /** Render a sortable header (requires `field`). */
  sortable?: boolean;
  /**
   * A per-column filter control, placed in the (column-aligned) filter row. The
   * control reads the grid context itself (via `useGrid`) to call `setFilter`.
   * The filter row renders only when at least one column supplies a control, so
   * a consumer toggles filters by supplying/withholding these nodes — no boolean
   * prop on the grid (architecture-avoid-boolean-props).
   */
  filter?: ReactNode;
  /** Applied to the header cell (e.g. column width). */
  className?: string;
  /** Applied to each body cell (e.g. `text-muted-foreground`). */
  cellClassName?: string;
  /** Per-row cell renderer. */
  cell: (row: T) => ReactNode;
}
