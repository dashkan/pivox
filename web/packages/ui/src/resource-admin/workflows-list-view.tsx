'use client';

import { Badge } from '@pivox/primitives/badge';

import { useGrid } from '../grid';

import { AdminSearch } from './admin-search';
import { actorLabel, formatTimestamp } from './meta-cells';
import { workflowLeafId, workflowVersionLabel } from './workflow-shared';

import type { GridColumn } from '../grid';
import type {
  ResourceColumnContext,
  ResourceListView,
} from './resource-list.context';
import type { Workflow, WorkflowListExtras, WorkflowOrigin } from './types';

/**
 * The workflows LIST view — the presentational descriptor `ResourceList` renders
 * from, the third-shape sibling of `connectorsListView` / `secretsListView`. The
 * data-side (`buildListRequest`, `rowId`, `rowsOf`) lives in the `@pivox/features`
 * `ListDescriptor`; this half lives here because it reads ui contexts (`useGrid`)
 * and composes ui atoms (`@pivox/ui` can't depend on `@pivox/features`).
 *
 * Unlike connectors/secrets, workflows have NO form: the Name link + row action
 * navigate to the bespoke React Flow CANVAS (`onEdit` → `/workflows/$id`), not a
 * routed form. And unlike them workflows are org-direct only (no space rollup, no
 * scope, no agent facet), so there is no Space column, no scope toolbar control,
 * and the extras bag is empty. This is a verbatim port of the columns that used
 * to live inline in the bespoke `<Table>` list.
 */

/** Origin badge: MANAGED workflows are Pivox-owned, OWNED are the customer's. */
function OriginBadge({ origin }: { origin?: WorkflowOrigin }) {
  return origin === 'MANAGED' ? (
    <Badge variant="secondary">Managed</Badge>
  ) : (
    <Badge variant="outline">Owned</Badge>
  );
}

/** Whether any name filter is active (workflows have no scope). */
function hasActiveFilters(filters: Record<string, string>): boolean {
  return Boolean(filters.displayName?.trim());
}

/**
 * Name filter control for the Name column's filter cell. Reads the grid context
 * (not the domain context) so it demonstrates the DI interface. Debounced text
 * commits with `replace` history so keystrokes don't stack entries.
 */
function WorkflowNameFilter() {
  const { state, actions } = useGrid<Workflow>();
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
 * Builds the workflow columns — Name (canvas link), Origin, Enabled, Live
 * version, Updated. The Name cell NAVIGATES to the canvas via `onEdit`; there is
 * no row edit/delete action column (workflows are edited on the canvas, not from
 * the list). Sortable columns match the server's order_by whitelist
 * (`displayName`, `updateTime`).
 */
function workflowColumns(
  ctx: ResourceColumnContext<Workflow, WorkflowListExtras>,
): GridColumn<Workflow>[] {
  const { showFilters, onEdit } = ctx;

  return [
    {
      field: 'displayName',
      header: 'Name',
      sortable: true,
      cellClassName: 'font-medium',
      filter: showFilters ? <WorkflowNameFilter /> : undefined,
      cell: (workflow) => (
        <button
          type="button"
          className="text-left hover:underline"
          onClick={() => onEdit(workflow)}
        >
          {workflow.displayName || workflowLeafId(workflow.name)}
        </button>
      ),
    },
    {
      header: 'Origin',
      cell: (workflow) => <OriginBadge origin={workflow.origin} />,
    },
    {
      header: 'Enabled',
      cellClassName: 'text-muted-foreground',
      cell: (workflow) => (workflow.enabled ? 'Enabled' : 'Disabled'),
    },
    {
      header: 'Live version',
      cellClassName: 'text-muted-foreground',
      cell: (workflow) => workflowVersionLabel(workflow.version),
    },
    {
      field: 'updateTime',
      header: 'Updated',
      sortable: true,
      cellClassName: 'text-muted-foreground',
      cell: (workflow) => (
        <>
          {formatTimestamp(workflow.updateTime)} ·{' '}
          {actorLabel(workflow.updatedBy)}
        </>
      ),
    },
  ];
}

/**
 * The workflows list view — data for the generic `ResourceList`. Note the absent
 * `newLabel`: workflows have no create-from-list flow (creation is a bespoke
 * canvas concern), so the shared frame renders no "New" button. `toolbar` is
 * likewise omitted (no scope).
 */
export const workflowsListView: ResourceListView<Workflow, WorkflowListExtras> =
  {
    title: 'Workflows',
    description: 'Authored and Pivox-managed workflow definitions.',
    loadingLabel: 'Loading workflows…',
    emptyLabel: (filtersActive) =>
      filtersActive ? 'No workflows match your filters.' : 'No workflows yet.',
    hasActiveFilters,
    rowKey: (workflow) => workflow.name ?? '',
    columns: workflowColumns,
    deleteConfirm: (workflow) => ({
      title: 'Delete workflow?',
      description: `This permanently deletes "${
        workflow?.displayName || workflowLeafId(workflow?.name)
      }".`,
    }),
  };
