'use client';

import { useMemo, useState } from 'react';

import { Grid, useGrid } from '../grid';

import { AdminFrame } from './admin-frame';
import { AdminSearch } from './admin-search';
import { ClearFiltersButton } from './clear-filters-button';
import { connectorSpaceSlug, spaceLabel } from './connector-shared';
import { DeleteDialog } from './delete-dialog';
import { FilterToggleButton } from './filter-toggle-button';
import { actorLabel, formatTimestamp } from './meta-cells';
import { RowActions } from './row-actions';
import { ScopeSelect } from './scope-select';
import { secretLeafId } from './secret-shared';
import { SecretsAdminContext, useSecretsAdmin } from './secrets-admin.context';

import type { GridColumn, GridContextValue } from '../grid';

import type {
  Secret,
  SecretsAdminContextValue,
  SpaceOption,
} from './types';

/** Whether any name filter or a non-default scope is active. */
function hasActiveFilters(
  filters: Record<string, string>,
  scope: string,
): boolean {
  return Boolean(filters.displayName?.trim()) || scope !== '';
}

function SecretsAdminProvider({
  value,
  children,
}: {
  value: SecretsAdminContextValue;
  children: React.ReactNode;
}) {
  return <SecretsAdminContext value={value}>{children}</SecretsAdminContext>;
}

/**
 * Bridges the secrets domain context into the generic `Grid` interface
 * (state-decouple-implementation) — the secret twin of `ConnectorsGridProvider`.
 * Scope is NOT a grid concept; it stays in the secrets domain context.
 */
function SecretsGridProvider({ children }: { children: React.ReactNode }) {
  const { state, actions } = useSecretsAdmin();
  const value = useMemo<GridContextValue<Secret>>(
    () => ({
      state: {
        rows: state.secrets,
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
      meta: { rowKey: (secret) => secret.name ?? '' },
    }),
    [state, actions],
  );
  return <Grid.Provider value={value}>{children}</Grid.Provider>;
}

/**
 * Name filter control for the Name column's filter cell. Reads the grid context
 * (not the domain context) so it demonstrates the DI interface. Debounced text
 * commits with `replace` history so keystrokes don't stack entries.
 */
function SecretNameFilter() {
  const { state, actions } = useGrid<Secret>();
  return (
    <AdminSearch
      value={state.filters.displayName ?? ''}
      onChange={(value) => actions.setFilter('displayName', value, 'replace')}
      placeholder="Filter by name"
      debounceMs={300}
    />
  );
}

/**
 * Builds the secret columns for `Grid.Table`. The Space column is spread in only
 * at the org rollup (`orgLevel`). The Name link + row Edit action NAVIGATE to the
 * routed edit page via `onEdit`; row delete opens the quick list-delete confirm
 * via `onRemove`. Sortable columns match the server's order_by fields
 * (`displayName`, `createTime`); Updated is display-only.
 */
function secretColumns(params: {
  orgLevel: boolean;
  showFilters: boolean;
  spaceOptions: SpaceOption[];
  onEdit: (secret: Secret) => void;
  onRemove: (secret: Secret) => void;
}): GridColumn<Secret>[] {
  const { orgLevel, showFilters, spaceOptions, onEdit, onRemove } = params;
  return [
    {
      field: 'displayName',
      header: 'Name',
      sortable: true,
      cellClassName: 'font-medium',
      filter: showFilters ? <SecretNameFilter /> : undefined,
      cell: (secret) => (
        <button
          type="button"
          className="text-left hover:underline"
          onClick={() => onEdit(secret)}
        >
          {secret.displayName || secretLeafId(secret.name)}
        </button>
      ),
    },
    ...(orgLevel
      ? ([
          {
            header: 'Space',
            cellClassName: 'text-muted-foreground',
            cell: (secret) =>
              connectorSpaceSlug(secret.name)
                ? spaceLabel(secret.name, spaceOptions)
                : '',
          },
        ] satisfies GridColumn<Secret>[])
      : []),
    {
      field: 'createTime',
      header: 'Created',
      sortable: true,
      cellClassName: 'text-muted-foreground',
      cell: (secret) => (
        <>
          {formatTimestamp(secret.createTime)} · {actorLabel(secret.createdBy)}
        </>
      ),
    },
    {
      header: 'Updated',
      cellClassName: 'text-muted-foreground',
      cell: (secret) => (
        <>
          {formatTimestamp(secret.updateTime)} · {actorLabel(secret.updatedBy)}
        </>
      ),
    },
    {
      header: '',
      className: 'w-0',
      cell: (secret) => (
        <RowActions
          editLabel="Edit secret"
          removeLabel="Delete secret"
          onEdit={() => onEdit(secret)}
          onRemove={() => onRemove(secret)}
        />
      ),
    },
  ];
}

function SecretsAdminRoot() {
  const { state, actions } = useSecretsAdmin();
  const { remove, spaceOptions, filters, scope } = state;
  const [showFilters, setShowFilters] = useState(false);
  const filtersActive = hasActiveFilters(filters, scope);
  const emptyLabel = filtersActive
    ? 'No secrets match your filters.'
    : 'No secrets yet.';

  return (
    <>
      <SecretsGridProvider>
        <AdminFrame
          title="Secrets"
          description="Encrypted credentials resolved by connectors at request time. Values are write-only."
          newLabel="New secret"
          onNew={actions.openCreate}
        >
          <Grid.Toolbar>
            <FilterToggleButton
              active={showFilters}
              onToggle={() => setShowFilters((v) => !v)}
            />
            {/* Scope is a secrets control the consumer wires into the toolbar,
                gated by the same filter toggle. The grid knows nothing about it. */}
            {showFilters ? (
              <ScopeSelect
                value={scope}
                spaces={spaceOptions}
                onChange={actions.setScope}
                allLabel="All spaces"
              />
            ) : null}
            {filtersActive ? (
              <ClearFiltersButton onClear={actions.clearFilters} />
            ) : null}
          </Grid.Toolbar>
          <Grid.Table
            columns={secretColumns({
              orgLevel: scope === '',
              showFilters,
              spaceOptions,
              onEdit: actions.openEdit,
              onRemove: actions.openRemove,
            })}
            emptyLabel={emptyLabel}
            loadingLabel="Loading secrets…"
          />
          <Grid.CursorPagination />
        </AdminFrame>
      </SecretsGridProvider>

      {/* Quick list-row delete stays a confirm dialog (the routed edit page has
          its own FormPage.Delete → the same DeleteDialog). */}
      <DeleteDialog
        open={remove.target !== null}
        onOpenChange={(open) => {
          if (!open) actions.closeRemove();
        }}
        title="Delete secret?"
        description={`This permanently deletes "${
          remove.target?.displayName || secretLeafId(remove.target?.name)
        }". A secret still referenced by a connector can't be deleted.`}
        error={remove.error}
        pending={remove.pending}
        onConfirm={actions.confirmRemove}
      />
    </>
  );
}

export const SecretsAdmin = {
  Provider: SecretsAdminProvider,
  Root: SecretsAdminRoot,
};
