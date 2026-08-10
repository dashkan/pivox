'use client';

import { Badge } from '@pivox/primitives/badge';

import { useGrid } from '../grid';

import { AdminSearch } from './admin-search';
import { AGENT_FILTER_ANY, AgentFilterSelect } from './agent-filter';
import {
  agentLabel,
  connectorSpaceSlug,
  connectorType,
  leafId,
  spaceLabel,
} from './connector-shared';
import { actorLabel, formatTimestamp } from './meta-cells';

import type { GridColumn } from '../grid';
import type {
  ResourceColumnContext,
  ResourceListView,
} from './resource-list.context';
import type { AgentOption, Connector, ConnectorListExtras } from './types';

/**
 * The connectors LIST view — the presentational descriptor `ResourceList`
 * renders from. This is the connectors half of the descriptor split: the
 * data-side (`buildListRequest`, scope paths, `rowId`, `rowsOf`) lives in the
 * `@pivox/features` `ListDescriptor`; this presentational half lives here because
 * it reads ui contexts (`useGrid`) and composes ui atoms (`@pivox/ui` can't
 * depend on `@pivox/features`). It is a verbatim port of the column + toolbar
 * logic that used to live inline in `ConnectorsAdmin`.
 */

/** Whether any in-list name/agent filter is active (scope is path-owned, not a
 *  clearable filter — the sidebar owns it). */
function hasActiveFilters(filters: Record<string, string>): boolean {
  return (
    Boolean(filters.displayName?.trim()) ||
    (filters.agent !== undefined && filters.agent !== AGENT_FILTER_ANY)
  );
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
 * Builds the connector CONTENT columns. The Space column is spread in only at the
 * org rollup — inside a specific space every row shares that space. Filter controls
 * are supplied only when `showFilters` is on; their presence is what makes the
 * grid render the filter row (composition, not a boolean grid prop). The Name
 * link NAVIGATES via `onEdit`. The trailing edit/delete affordance column is NOT
 * built here — it is composed on top via `actionsColumn` (see `ResourceList.Root`/
 * `ResourceList.Default`), so create/delete stay presence-driven.
 */
function connectorColumns(
  ctx: ResourceColumnContext<Connector, ConnectorListExtras>,
): GridColumn<Connector>[] {
  const { scope, showFilters, extras, onEdit } = ctx;
  const { agentOptions, agentsInUse, spaceOptions } = extras;
  const orgLevel = scope === '';
  // The agent facet lists only agents actually in scope, resolved to labels via
  // the full agent list (leaf fallback). Hidden when every connector in scope
  // runs on the cloud (no agents in use).
  const inUseAgentOptions = agentsInUse.map((name) => ({
    value: name,
    label: agentLabel(name, agentOptions),
  }));

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
            cell: (connector: Connector) =>
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
  ];
}

/** The connectors list view — data for the generic `ResourceList`. */
export const connectorsListView: ResourceListView<
  Connector,
  ConnectorListExtras
> = {
  title: 'Connectors',
  description:
    'Reusable, credentialed connections to external systems, used by workflow activities.',
  loadingLabel: 'Loading connectors…',
  emptyLabel: (filtersActive) =>
    filtersActive ? 'No connectors match your filters.' : 'No connectors yet.',
  hasActiveFilters,
  rowKey: (connector) => connector.name ?? '',
  columns: connectorColumns,
  // No in-list scope control: the sidebar scope picker owns org/space scope (the
  // URL is the source of truth). The org rollup keeps the Space column so each
  // row's space is visible; narrowing to a space is a sidebar navigation.
};

/**
 * The connectors delete-confirm description — the resource-specific copy the
 * composed delete affordance carries (`ResourceList.Default`'s `confirmDelete`).
 * The title ("Delete connector?") is derived from the noun; only this warning is
 * bespoke.
 */
export function connectorDeleteDescription(connector: Connector): string {
  return `This permanently deletes "${
    connector.displayName || leafId(connector.name)
  }". Activities that reference it will fail.`;
}
