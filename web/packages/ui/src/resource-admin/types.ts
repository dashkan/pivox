import type { components } from '@pivox/client/types';
// The generic list-control types are owned by the lower `grid` tier; re-export
// them here so resource-admin consumers keep a single import surface.
import type { HistoryMode, SortState } from '../grid/types';

export type { HistoryMode, SortDirection, SortState } from '../grid/types';

export type Connector = components['schemas']['v1Connector'];
export type Secret = components['schemas']['v1Secret'];
export type Actor = components['schemas']['typesActor'];

export type DialogMode = 'create' | 'edit';

/**
 * Serializable list-controls state. The route owns this (URL search params) and
 * feeds it to a controlled `useListControls`, so the packages stay router-free.
 */
export interface ListControlsValue {
  /** Per-field filter values (e.g. `displayName`, `agent`). */
  filters: Record<string, string>;
  /** Active column sort, or null for the server default. */
  sort: SortState | null;
  /** Rows requested per page. */
  pageSize: number;
  /** List parent: empty is the org rollup; non-empty is a space slug. */
  scope: string;
  /** Opaque page cursor; undefined is the first page. */
  pageToken: string | undefined;
}

/** Commits a new controls value, telling the route how to record it in history. */
export type ListControlsChange = (
  next: ListControlsValue,
  opts: { history: HistoryMode },
) => void;

/** Filter / sort / pagination surface a list admin exposes to its table. */
export interface ListControlsState {
  /** Immediate per-field filter values (e.g. `displayName`, `agent`). */
  filters: Record<string, string>;
  /** Active column sort, or null for the server default. */
  sort: SortState | null;
  /** Rows requested per page. */
  pageSize: number;
  /**
   * Which parent the list targets: the empty string is the org rollup; a
   * non-empty value is a space slug. Switches the query path, not the filter.
   */
  scope: string;
  pagination: {
    hasPrevPage: boolean;
    hasNextPage: boolean;
  };
}

export interface ListControlsActions {
  /**
   * Set (or clear, with '') one field's filter. `history` defaults to 'push';
   * the debounced search text passes 'replace' so keystrokes don't spam history.
   */
  setFilter: (field: string, value: string, history?: HistoryMode) => void;
  /** Reset all filters and scope (not sort) in one action. */
  clearFilters: () => void;
  toggleSort: (field: string) => void;
  setPageSize: (size: number) => void;
  setScope: (scope: string) => void;
  nextPage: () => void;
  prevPage: () => void;
}

/** One row of the connector-headers / annotations map editor. */
export interface KeyValueEntry {
  key: string;
  value: string;
}

export interface ConnectorFormValues {
  /** User-assigned id, unique within the parent. Create-only (immutable). */
  connectorId: string;
  displayName: string;
  description: string;
  baseUrl: string;
  headers: KeyValueEntry[];
  /** On-prem agent resource name; empty routes to the cloud controller. */
  agent: string;
  /**
   * Target scope: empty creates an org-direct connector; a space slug creates
   * it under that space. Create-only (a connector can't move scope).
   */
  scope: string;
}

/** A selectable on-prem agent for the connector "Run on Agent" dropdown. */
export interface AgentOption {
  /** Agent resource name; the empty string is not represented (that is "none"). */
  value: string;
  label: string;
}

/** A selectable space for the connector scope dropdown. */
export interface SpaceOption {
  /** Space resource name (`organizations/{org}/spaces/{slug}`). */
  name: string;
  /** The space slug (resource-name leaf); the `{space}` path param. */
  slug: string;
  displayName: string;
}

export interface SecretFormValues {
  /** User-assigned id, unique within the parent. Create-only (immutable). */
  secretId: string;
  displayName: string;
  annotations: KeyValueEntry[];
  /**
   * The opaque value. Required on create. On edit it is written only when
   * `rotate` is set — otherwise the stored value is left untouched.
   */
  value: string;
  /** Edit-only: whether to rotate `value`. Ignored on create (always set). */
  rotate: boolean;
  /**
   * Target scope: empty creates an org-direct secret; a space slug creates it
   * under that space. Create-only (a secret can't move scope), mirroring
   * connectors.
   */
  scope: string;
}

/** Transient dialog state shared by both resource admins. */
export interface DialogState<Row> {
  open: boolean;
  mode: DialogMode;
  /** The row being edited, or null in create mode. */
  editing: Row | null;
  error: string | null;
  pending: boolean;
}

/** Transient delete-confirmation state shared by both resource admins. */
export interface RemoveState<Row> {
  target: Row | null;
  error: string | null;
  pending: boolean;
}

/**
 * The connectors-specific `extras` carried on the generic resource-list value
 * (`ResourceListContextValue<Connector, ConnectorListExtras>`): the assignable
 * agents (create form), the in-scope agents (filter facet), and the spaces to
 * scope by. The connectors list view's columns/toolbar read these.
 */
export interface ConnectorListExtras {
  /** All assignable on-prem agents (create form); empty = only the cloud option. */
  agentOptions: AgentOption[];
  /** Distinct agents in the base scope, for the filter facet; empty = hide it. */
  agentsInUse: string[];
  /** Spaces to scope by (filter) or create into; empty = org-only. */
  spaceOptions: SpaceOption[];
}

/**
 * The secrets-specific `extras` carried on the generic resource-list value
 * (`ResourceListContextValue<Secret, SecretListExtras>`): just the spaces to
 * scope by. The secrets list view's columns (Space label) + toolbar (scope
 * picker) read these. Secrets have no per-response facet (no agent equivalent),
 * so this is the whole extras bag.
 */
export interface SecretListExtras {
  /** Spaces to scope by (filter) or resolve a space-scoped row's label; empty = org-only. */
  spaceOptions: SpaceOption[];
}
