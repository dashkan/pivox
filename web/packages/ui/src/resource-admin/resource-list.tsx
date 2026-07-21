'use client';

import { useMemo, useState } from 'react';

import { Grid } from '../grid';

import { AdminFrame } from './admin-frame';
import { ClearFiltersButton } from './clear-filters-button';
import { DeleteDialog } from './delete-dialog';
import { FilterToggleButton } from './filter-toggle-button';
import {
  ResourceListContext,
  useResourceListContext,
} from './resource-list.context';

import type { GridContextValue } from '../grid';
import type {
  ResourceListContextValue,
  ResourceListView,
} from './resource-list.context';
import type { ReactNode } from 'react';

/**
 * Generic, descriptor-driven resource LIST — the compound that subsumes the
 * hand-written `ConnectorsAdmin`. A `ResourceList.Provider` injects the DI'd
 * `ResourceListContextValue<Row, Extras>` (produced by `useResourceList` in
 * `@pivox/features`); `ResourceList.Root` renders the admin chrome + the generic
 * `Grid` from a presentational `ResourceListView`. Every resource gets the same
 * List by supplying a value + a view — no per-resource grid bridge.
 *
 * ```tsx
 * <ResourceList.Provider value={value}>
 *   <ResourceList.Root view={connectorsListView} />
 * </ResourceList.Provider>
 * ```
 *
 * The Root owns exactly one piece of local UI state — the filter-row toggle —
 * exactly as `ConnectorsAdmin.Root` did; everything else is injected. It bridges
 * the resource value onto `GridContextValue` (state-decouple-implementation): the
 * one place list state/actions become the grid interface, so the grid stays
 * domain-blind. The view's `toolbar` supplies resource-specific controls (scope),
 * gated by the same filter toggle; the view's `columns` carry per-column filters
 * so the grid renders the filter row only when the view provides them
 * (architecture-avoid-boolean-props).
 */

function ResourceListProvider<Row, Extras>({
  value,
  children,
}: {
  value: ResourceListContextValue<Row, Extras>;
  children: ReactNode;
}) {
  // eslint-disable-next-line typescript/no-unsafe-type-assertion -- widen the consumer's typed value to the unknown-rowed context (React context is invariant); useResourceListContext<Row, Extras> re-narrows. The DI boundary needs this cast.
  const injected = value as ResourceListContextValue<unknown, unknown>;
  return (
    <ResourceListContext value={injected}>{children}</ResourceListContext>
  );
}

function ResourceListRoot<Row, Extras>({
  view,
}: {
  view: ResourceListView<Row, Extras>;
}) {
  const { state, actions } = useResourceListContext<Row, Extras>();
  const [showFilters, setShowFilters] = useState(false);
  const filtersActive = view.hasActiveFilters(state.filters, state.scope);

  // Bridge the resource value onto the generic grid interface — the only place
  // that knows how list state is produced. Scope, extras, and the delete-confirm
  // deliberately stay OUT of the grid; they live in the resource tier.
  const gridValue = useMemo<GridContextValue<Row>>(
    () => ({
      state: {
        rows: state.rows,
        isLoading: state.isLoading,
        loadError: state.loadError,
        filters: state.filters,
        sort: state.sort,
        pageSize: state.pageSize,
        pagination: {
          hasPrev: state.pagination.hasPrevPage,
          hasNext: state.pagination.hasNextPage,
        },
      },
      actions: {
        setFilter: actions.setFilter,
        toggleSort: actions.toggleSort,
        setPageSize: actions.setPageSize,
        clearFilters: actions.clearFilters,
        nextPage: actions.nextPage,
        prevPage: actions.prevPage,
      },
      meta: { rowKey: view.rowKey },
    }),
    [state, actions, view],
  );

  const columns = view.columns({
    scope: state.scope,
    showFilters,
    extras: state.extras,
    onEdit: actions.openEdit,
    onRemove: actions.openRemove,
  });

  const confirm = view.deleteConfirm(state.remove.target);

  return (
    <>
      <Grid.Provider value={gridValue}>
        <AdminFrame
          title={view.title}
          description={view.description}
          newLabel={view.newLabel}
          onNew={actions.openCreate}
        >
          <Grid.Toolbar>
            <FilterToggleButton
              active={showFilters}
              onToggle={() => setShowFilters((v) => !v)}
            />
            {view.toolbar?.({
              scope: state.scope,
              showFilters,
              extras: state.extras,
              setScope: actions.setScope,
            })}
            {filtersActive ? (
              <ClearFiltersButton onClear={actions.clearFilters} />
            ) : null}
          </Grid.Toolbar>
          <Grid.Table
            columns={columns}
            emptyLabel={view.emptyLabel(filtersActive)}
            loadingLabel={view.loadingLabel}
          />
          <Grid.CursorPagination />
        </AdminFrame>
      </Grid.Provider>

      {/* Quick list-row delete stays a confirm dialog (the routed edit page has
          its own FormPage.Delete → the same DeleteDialog). */}
      <DeleteDialog
        open={state.remove.target !== null}
        onOpenChange={(open) => {
          if (!open) actions.closeRemove();
        }}
        title={confirm.title}
        description={confirm.description}
        error={state.remove.error}
        pending={state.remove.pending}
        onConfirm={actions.confirmRemove}
      />
    </>
  );
}

/** The compound descriptor-driven list. Consumers supply a value + a view. */
export const ResourceList = {
  Provider: ResourceListProvider,
  Root: ResourceListRoot,
};
