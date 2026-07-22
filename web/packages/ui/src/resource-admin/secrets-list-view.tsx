'use client';

import { useGrid } from '../grid';

import { AdminSearch } from './admin-search';
import { connectorSpaceSlug, spaceLabel } from './connector-shared';
import { actorLabel, formatTimestamp } from './meta-cells';
import { secretLeafId } from './secret-shared';

import type { GridColumn } from '../grid';
import type {
  ResourceColumnContext,
  ResourceListView,
} from './resource-list.context';
import type { Secret, SecretListExtras } from './types';

/**
 * The secrets LIST view — the presentational descriptor `ResourceList` renders
 * from, the secret twin of `connectorsListView`. The data-side (`buildListRequest`,
 * scope paths, `rowId`, `rowsOf`) lives in the `@pivox/features` `ListDescriptor`;
 * this half lives here because it reads ui contexts (`useGrid`) and composes ui
 * atoms (`@pivox/ui` can't depend on `@pivox/features`). It is a verbatim port of
 * the column + toolbar logic that used to live inline in `SecretsAdmin`.
 *
 * The secret VALUE is write-only (INPUT_ONLY) — the API never returns it, so no
 * column ever surfaces it; the list is metadata-only.
 */

/** Whether any in-list name filter is active (scope is path-owned, not a
 *  clearable filter — the sidebar owns it). */
function hasActiveFilters(filters: Record<string, string>): boolean {
  return Boolean(filters.displayName?.trim());
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
 * Builds the secret CONTENT columns. The Space column is spread in only at the org
 * rollup — inside a specific space every row shares that space. Filter controls
 * are supplied only when `showFilters` is on; their presence is what makes the
 * grid render the filter row (composition, not a boolean grid prop). The Name
 * link NAVIGATES via `onEdit`. The trailing edit/delete affordance column is NOT
 * built here — it is composed on top via `actionsColumn`. Sortable columns match
 * the server's order_by fields (`displayName`, `createTime`); Updated is
 * display-only.
 */
function secretColumns(
  ctx: ResourceColumnContext<Secret, SecretListExtras>,
): GridColumn<Secret>[] {
  const { scope, showFilters, extras, onEdit } = ctx;
  const { spaceOptions } = extras;
  const orgLevel = scope === '';

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
            cell: (secret: Secret) =>
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
  ];
}

/** The secrets list view — data for the generic `ResourceList`. */
export const secretsListView: ResourceListView<Secret, SecretListExtras> = {
  title: 'Secrets',
  description:
    'Encrypted credentials resolved by connectors at request time. Values are write-only.',
  loadingLabel: 'Loading secrets…',
  emptyLabel: (filtersActive) =>
    filtersActive ? 'No secrets match your filters.' : 'No secrets yet.',
  hasActiveFilters,
  rowKey: (secret) => secret.name ?? '',
  columns: secretColumns,
  // No in-list scope control: the sidebar scope picker owns org/space scope. The
  // org rollup keeps the Space column so each row's space is visible; narrowing
  // to a space is a sidebar navigation.
};

/**
 * The secrets delete-confirm description — the resource-specific copy the composed
 * delete affordance carries (`ResourceList.Default`'s `confirmDelete`). The title
 * ("Delete secret?") is derived from the noun; only this warning is bespoke.
 */
export function secretDeleteDescription(secret: Secret): string {
  return `This permanently deletes "${
    secret.displayName || secretLeafId(secret.name)
  }". A secret still referenced by a connector can't be deleted.`;
}
