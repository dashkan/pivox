'use client';

import { Button } from '@pivox/primitives/button';
import { PlusIcon } from 'lucide-react';
import { useMemo, useState } from 'react';

import { Grid } from '../grid';

import { actionsColumn } from './actions-column';
import { AdminFrame } from './admin-frame';
import { ClearFiltersButton } from './clear-filters-button';
import { DeleteDialog } from './delete-dialog';
import { FilterToggleButton } from './filter-toggle-button';
import {
  ResourceListContext,
  useResourceListContext,
} from './resource-list.context';

import type { ActionsColumnOptions } from './actions-column';
import type { GridContextValue } from '../grid';
import type {
  ResourceColumnContext,
  ResourceListContextValue,
  ResourceListView,
} from './resource-list.context';
import type { ReactNode } from 'react';

/**
 * Generic, descriptor-driven resource LIST — the compound that subsumes the
 * hand-written `ConnectorsAdmin`. A `ResourceList.Provider` injects the DI'd
 * `ResourceListContextValue<Row, Extras>` (produced by `useResourceList` in
 * `@pivox/features`); `ResourceList.Root` renders the admin chrome + the generic
 * `Grid` from a presentational `ResourceListView`.
 *
 * Affordances are COMPOSED, not descriptor flags (§2b of resource-admin-dry):
 *
 * - The New button (create) is a composed child — `ResourceList.NewButton` inside
 *   a `ResourceList.Toolbar`. Presence of the child is the config; there is no
 *   `newLabel` field and `AdminFrame` renders no button itself.
 * - The row edit/delete affordances are the {@link actionsColumn} composed onto
 *   the view's content columns via `Root`'s `actions` opts. Presence of `actions`
 *   is the config; presence of `actions.delete` additionally wires the confirm
 *   dialog. There is no `deleteConfirm` field.
 *
 * `ResourceList.Default` composes both for the 90% case (New + edit+delete) in one
 * line; resources compose explicitly only to deviate (workflows compose neither).
 *
 * ```tsx
 * // 90% case
 * <ResourceList.Provider value={value}>
 *   <ResourceList.Default view={connectorsListView} noun="connector"
 *     confirmDelete={(c) => `…`} />
 * </ResourceList.Provider>
 *
 * // list + filter toolbar only (workflows)
 * <ResourceList.Provider value={value}>
 *   <ResourceList.Root view={workflowsListView} />
 * </ResourceList.Provider>
 * ```
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

/**
 * Container for composed header-actions (top-right of the frame). Today it holds
 * the `ResourceList.NewButton`; it's the composition seam for any future primary
 * actions (import, bulk, …) — presence of a child is the affordance.
 */
function ResourceListToolbar({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-2">{children}</div>;
}

/**
 * The composed create affordance. Presence of this child IS the "resource has
 * create" config — no descriptor flag. It reads the injected list value for the
 * routed `openCreate` navigation, so the button stays router-agnostic.
 */
function ResourceListNewButton({ label }: { label: string }) {
  const { actions } = useResourceListContext<unknown, unknown>();
  return (
    <Button onClick={actions.openCreate}>
      <PlusIcon />
      {label}
    </Button>
  );
}

function ResourceListRoot<Row, Extras>({
  view,
  actions: actionOpts,
  children,
}: {
  view: ResourceListView<Row, Extras>;
  /**
   * The edit/delete affordance column opts. Omit for none (workflows); supply
   * `{ edit, delete: { confirm } }` to compose the {@link actionsColumn} and — when
   * `delete` is present — the confirm dialog. This is the sanctioned opt-object
   * exception; the create/delete coupling stays dissolved (presence-driven).
   */
  actions?: ActionsColumnOptions<Row>;
  /** Composed header-actions (e.g. `ResourceList.Toolbar` + `NewButton`). */
  children?: ReactNode;
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

  const columnContext: ResourceColumnContext<Row, Extras> = {
    scope: state.scope,
    showFilters,
    extras: state.extras,
    onEdit: actions.openEdit,
    openRemove: actions.openRemove,
  };

  // Content columns (view, columns-as-data) + the composed edit/delete affordance
  // column when the resource opts into it. Composition, not a descriptor flag.
  const columns = actionOpts
    ? [...view.columns(columnContext), actionsColumn(columnContext, actionOpts)]
    : view.columns(columnContext);

  const target = state.remove.target;
  const confirm =
    actionOpts?.delete && target ? actionOpts.delete.confirm(target) : null;

  return (
    <>
      <Grid.Provider value={gridValue}>
        <AdminFrame
          title={view.title}
          description={view.description}
          actions={children}
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

      {/* The confirm dialog + `remove` mutation are a composite concern; it only
          exists when a delete affordance was composed in. The routed edit page has
          its own FormPage.Delete → the same DeleteDialog. */}
      {actionOpts?.delete ? (
        <DeleteDialog
          open={target !== null}
          onOpenChange={(open) => {
            if (!open) actions.closeRemove();
          }}
          title={confirm?.title ?? ''}
          description={confirm?.description ?? ''}
          error={state.remove.error}
          pending={state.remove.pending}
          onConfirm={actions.confirmRemove}
        />
      ) : null}
    </>
  );
}

/**
 * The 90% preset: New button (create) + an edit+delete affordance column + the
 * confirm dialog, composed in one line. `noun` drives the label copy
 * ("New {noun}" / "Edit {noun}" / "Delete {noun}" / "Delete {noun}?"); the
 * per-row confirm description is the only resource-specific copy left. A resource
 * that needs to deviate (edit-only, a custom title, extra header actions) drops
 * `Default` and composes `Root` + `actionsColumn` directly.
 */
function ResourceListDefault<Row, Extras>({
  view,
  noun,
  confirmDelete,
}: {
  view: ResourceListView<Row, Extras>;
  /** Singular resource noun for the composed labels, e.g. `'connector'`. */
  noun: string;
  /** The per-row confirm description (the title is `Delete {noun}?`). */
  confirmDelete: (row: Row) => string;
}) {
  return (
    <ResourceListRoot
      view={view}
      actions={{
        edit: true,
        editLabel: `Edit ${noun}`,
        removeLabel: `Delete ${noun}`,
        delete: {
          confirm: (row) => ({
            title: `Delete ${noun}?`,
            description: confirmDelete(row),
          }),
        },
      }}
    >
      <ResourceListToolbar>
        <ResourceListNewButton label={`New ${noun}`} />
      </ResourceListToolbar>
    </ResourceListRoot>
  );
}

/** The compound descriptor-driven list. Consumers supply a value + compose. */
export const ResourceList = {
  Provider: ResourceListProvider,
  Root: ResourceListRoot,
  Toolbar: ResourceListToolbar,
  NewButton: ResourceListNewButton,
  Default: ResourceListDefault,
};
