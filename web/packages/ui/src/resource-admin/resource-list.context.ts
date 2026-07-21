'use client';

import { createContext, use } from 'react';

import type { GridColumn } from '../grid';
import type {
  ListControlsActions,
  ListControlsState,
  RemoveState,
} from './types';
import type { ReactNode } from 'react';

/**
 * Generic, DI'd resource-LIST interface — the descriptor-driven twin of the
 * `Grid` context, one tier up. `ResourceList` (the compound) reads this via
 * `useResourceListContext`; a `useResourceList` hook in `@pivox/features`
 * produces it. It is a strict superset of what `Grid` needs: the shared
 * list-controls (filters/sort/scope/pagination) PLUS the row-delete confirm and
 * a resource-specific `extras` bag (agents/spaces/facets) the view's columns and
 * toolbar read. The connectors admin's bespoke `ConnectorsAdminContextValue` is
 * exactly this with `Extras = ConnectorListExtras`.
 */
export interface ResourceListState<Row, Extras> extends ListControlsState {
  rows: Row[];
  isLoading: boolean;
  /** A load-error message to show in the body, or null. */
  loadError: string | null;
  /** The quick list-row delete confirmation (the routed edit page has its own). */
  remove: RemoveState<Row>;
  /**
   * Resource-specific data the view's columns/toolbar need but the generic list
   * can't derive from a row (assignable agents, in-use agents, spaces to scope
   * by). Injected by the feature; the grid never sees it.
   */
  extras: Extras;
}

/**
 * The write half. `openCreate`/`openEdit` NAVIGATE (routed create/edit pages);
 * the delete-confirm quartet drives the quick list-row delete dialog. The
 * remaining actions are the shared `ListControlsActions`.
 */
export interface ResourceListActions<Row> extends ListControlsActions {
  openCreate: () => void;
  openEdit: (row: Row) => void;
  openRemove: (row: Row) => void;
  closeRemove: () => void;
  confirmRemove: () => void;
}

export interface ResourceListContextValue<Row, Extras> {
  state: ResourceListState<Row, Extras>;
  actions: ResourceListActions<Row>;
}

/**
 * Runtime context handed to a view's `columns` builder: the current scope + the
 * filter-row toggle + the resource extras + the routed edit/remove callbacks.
 * Columns-as-data (the sanctioned children-over-render carve-out) means the view
 * returns a `GridColumn<Row>[]` from this.
 */
export interface ResourceColumnContext<Row, Extras> {
  /** Empty is the org rollup; a non-empty value is a space slug. */
  scope: string;
  /** Whether the filter row is showing (supply column `filter` nodes only then). */
  showFilters: boolean;
  extras: Extras;
  onEdit: (row: Row) => void;
  onRemove: (row: Row) => void;
}

/** Runtime context for a view's resource-specific toolbar controls (e.g. scope). */
export interface ResourceToolbarContext<Extras> {
  scope: string;
  showFilters: boolean;
  extras: Extras;
  setScope: (scope: string) => void;
}

/**
 * A resource's LIST view config — the presentational descriptor `ResourceList`
 * renders from. Pure data + builder closures (no state, no queries): the title
 * chrome, the columns-as-data builder, the resource-specific toolbar controls,
 * the empty/loading copy, and the delete-confirm copy. Lives in `@pivox/ui`
 * because it's presentational; the `@pivox/features` `ListDescriptor` references
 * it (ui can't depend on features).
 */
export interface ResourceListView<Row, Extras> {
  title: string;
  description: string;
  /**
   * "New connector" — the create-button label. OPTIONAL: a create-less resource
   * (workflows, whose creation is a bespoke canvas concern, not a list action)
   * omits it and the shared frame renders no "New" button. Present for every
   * form-backed resource.
   */
  newLabel?: string;
  loadingLabel: string;
  /** Body copy for a zero-row result; `filtersActive` distinguishes filtered-empty. */
  emptyLabel: (filtersActive: boolean) => string;
  /** Whether any filter or a non-default scope is active (drives Clear + empty copy). */
  hasActiveFilters: (filters: Record<string, string>, scope: string) => boolean;
  /** Stable React key for a row (the grid's `meta.rowKey`). */
  rowKey: (row: Row) => string;
  /** Columns-as-data, built with the runtime column context. */
  columns: (ctx: ResourceColumnContext<Row, Extras>) => GridColumn<Row>[];
  /** Resource-specific toolbar controls (scope select, gated by `showFilters`). */
  toolbar?: (ctx: ResourceToolbarContext<Extras>) => ReactNode;
  /** Copy for the quick list-row delete dialog. */
  deleteConfirm: (row: Row | null) => { title: string; description: string };
}

export const ResourceListContext =
  createContext<ResourceListContextValue<unknown, unknown> | null>(null);

export function useResourceListContext<Row, Extras>(): ResourceListContextValue<
  Row,
  Extras
> {
  const ctx = use(ResourceListContext);
  if (!ctx) {
    throw new Error(
      'ResourceList subcomponents must be used within a ResourceList.Provider',
    );
  }
  // eslint-disable-next-line typescript/no-unsafe-type-assertion -- re-narrow the unknown-rowed context (React context is invariant); the Provider widened the consumer's typed value symmetrically.
  return ctx as ResourceListContextValue<Row, Extras>;
}
