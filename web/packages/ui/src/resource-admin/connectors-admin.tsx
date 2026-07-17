'use client';

import { Badge } from '@pivox/primitives/badge';
import { useMemo, useState } from 'react';

import { Grid, useGrid } from '../grid';

import { AdminFrame } from './admin-frame';
import { AdminSearch } from './admin-search';
import { AGENT_FILTER_ANY, AgentFilterSelect } from './agent-filter';
import { ClearFiltersButton } from './clear-filters-button';
import {
  agentLabel,
  connectorSpaceSlug,
  connectorType,
  leafId,
  spaceLabel,
} from './connector-shared';
import {
  ConnectorsAdminContext,
  useConnectorsAdmin,
} from './connectors-admin.context';
import { DeleteDialog } from './delete-dialog';
import { FilterToggleButton } from './filter-toggle-button';
import { actorLabel, formatTimestamp } from './meta-cells';
import { RowActions } from './row-actions';
import { ScopeSelect } from './scope-select';

import type { GridColumn, GridContextValue } from '../grid';

import type {
  AgentOption,
  Connector,
  ConnectorsAdminContextValue,
  SpaceOption,
} from './types';

/** Whether any name/agent filter or a non-default scope is active. */
function hasActiveFilters(
  filters: Record<string, string>,
  scope: string,
): boolean {
  return (
    Boolean(filters.displayName?.trim()) ||
    (filters.agent !== undefined && filters.agent !== AGENT_FILTER_ANY) ||
    scope !== ''
  );
}

function ConnectorsAdminProvider({
  value,
  children,
}: {
  value: ConnectorsAdminContextValue;
  children: React.ReactNode;
}) {
  return (
    <ConnectorsAdminContext value={value}>{children}</ConnectorsAdminContext>
  );
}

/**
 * Bridges the connectors domain context into the generic `Grid` interface
 * (state-decouple-implementation): the only place that maps connectors list
 * state/actions onto `GridContextValue<Connector>`. Every `Grid.*` part below
 * reads that injected interface — the grid never sees a connector concept.
 */
function ConnectorsGridProvider({ children }: { children: React.ReactNode }) {
  const { state, actions } = useConnectorsAdmin();
  const value = useMemo<GridContextValue<Connector>>(
    () => ({
      state: {
        rows: state.connectors,
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
      // Scope is NOT a grid concept — it stays in the connectors domain context.
      meta: { rowKey: (connector) => connector.name ?? '' },
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
function ConnectorNameFilter() {
  const { state, actions } = useGrid<Connector>();
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
 * Agent facet control for the Agent column's filter cell. `options` (agents in
 * the base scope, label-resolved) come from the connectors consumer — the grid
 * has no agent concept; the setFilter wiring comes from the grid context.
 */
function ConnectorAgentFilter({ options }: { options: AgentOption[] }) {
  const { state, actions } = useGrid<Connector>();
  return (
    <AgentFilterSelect
      value={state.filters.agent ?? AGENT_FILTER_ANY}
      // Discrete selection: push history so Back returns to the prior facet.
      onChange={(value) => actions.setFilter('agent', value, 'push')}
      options={options}
    />
  );
}

/**
 * Builds the connector columns for `Grid.Table`. The Space column is spread in
 * only at the org rollup (`orgLevel`) — inside a specific space every row shares
 * that space, so the column is redundant. Filter controls are supplied only when
 * `showFilters` is on; their presence is what makes the grid render the filter
 * row (composition, not a boolean grid prop). The Name link + row Edit action
 * NAVIGATE to the routed edit page via `onEdit`; row delete opens the quick
 * list-delete confirm via `onRemove`.
 */
function connectorColumns(params: {
  orgLevel: boolean;
  showFilters: boolean;
  agentOptions: AgentOption[];
  spaceOptions: SpaceOption[];
  inUseAgentOptions: AgentOption[];
  onEdit: (connector: Connector) => void;
  onRemove: (connector: Connector) => void;
}): GridColumn<Connector>[] {
  const {
    orgLevel,
    showFilters,
    agentOptions,
    spaceOptions,
    inUseAgentOptions,
    onEdit,
    onRemove,
  } = params;
  return [
    {
      field: 'displayName',
      header: 'Name',
      sortable: true,
      cellClassName: 'font-medium',
      filter: showFilters ? <ConnectorNameFilter /> : undefined,
      cell: (connector) => (
        <button
          type="button"
          className="text-left hover:underline"
          onClick={() => onEdit(connector)}
        >
          {connector.displayName || leafId(connector.name)}
        </button>
      ),
    },
    ...(orgLevel
      ? ([
          {
            header: 'Space',
            cellClassName: 'text-muted-foreground',
            cell: (connector) =>
              connectorSpaceSlug(connector.name)
                ? spaceLabel(connector.name, spaceOptions)
                : '',
          },
        ] satisfies GridColumn<Connector>[])
      : []),
    {
      header: 'Type',
      cell: (connector) => {
        const type = connectorType(connector);
        return type ? <Badge variant="secondary">{type}</Badge> : '—';
      },
    },
    {
      header: 'Agent',
      cellClassName: 'text-muted-foreground',
      filter:
        showFilters && inUseAgentOptions.length > 0 ? (
          <ConnectorAgentFilter options={inUseAgentOptions} />
        ) : undefined,
      cell: (connector) => agentLabel(connector.agent, agentOptions),
    },
    {
      field: 'updateTime',
      header: 'Updated',
      sortable: true,
      cellClassName: 'text-muted-foreground',
      cell: (connector) => (
        <>
          {formatTimestamp(connector.updateTime)} ·{' '}
          {actorLabel(connector.updatedBy)}
        </>
      ),
    },
    {
      header: '',
      className: 'w-0',
      cell: (connector) => (
        <RowActions
          editLabel="Edit connector"
          removeLabel="Delete connector"
          onEdit={() => onEdit(connector)}
          onRemove={() => onRemove(connector)}
        />
      ),
    },
  ];
}

function ConnectorsAdminRoot() {
  const { state, actions } = useConnectorsAdmin();
  const { remove, agentOptions, spaceOptions, filters, scope } = state;
  const [showFilters, setShowFilters] = useState(false);
  const filtersActive = hasActiveFilters(filters, scope);
  // The agent facet lists only agents actually in scope, resolved to labels via
  // the full agent list (leaf fallback). Hidden when every connector in scope
  // runs on the cloud (no agents in use).
  const inUseAgentOptions = state.agentsInUse.map((name) => ({
    value: name,
    label: agentLabel(name, agentOptions),
  }));
  const emptyLabel = filtersActive
    ? 'No connectors match your filters.'
    : 'No connectors yet.';

  return (
    <>
      <ConnectorsGridProvider>
        <AdminFrame
          title="Connectors"
          description="Reusable, credentialed connections to external systems, used by workflow activities."
          newLabel="New connector"
          onNew={actions.openCreate}
        >
          <Grid.Toolbar>
            <FilterToggleButton
              active={showFilters}
              onToggle={() => setShowFilters((v) => !v)}
            />
            {/* Scope is a connectors control the consumer wires into the toolbar,
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
            columns={connectorColumns({
              orgLevel: scope === '',
              showFilters,
              agentOptions,
              spaceOptions,
              inUseAgentOptions,
              onEdit: actions.openEdit,
              onRemove: actions.openRemove,
            })}
            emptyLabel={emptyLabel}
            loadingLabel="Loading connectors…"
          />
          <Grid.CursorPagination />
        </AdminFrame>
      </ConnectorsGridProvider>

      {/* Quick list-row delete stays a confirm dialog (the routed edit page has
          its own FormPage.Delete → the same DeleteDialog). */}
      <DeleteDialog
        open={remove.target !== null}
        onOpenChange={(open) => {
          if (!open) actions.closeRemove();
        }}
        title="Delete connector?"
        description={`This permanently deletes "${
          remove.target?.displayName || leafId(remove.target?.name)
        }". Activities that reference it will fail.`}
        error={remove.error}
        pending={remove.pending}
        onConfirm={actions.confirmRemove}
      />
    </>
  );
}

export const ConnectorsAdmin = {
  Provider: ConnectorsAdminProvider,
  Root: ConnectorsAdminRoot,
};
