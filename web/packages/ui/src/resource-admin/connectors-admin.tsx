'use client';

import { parseResourceName } from '@pivox/client';
import { Badge } from '@pivox/primitives/badge';
import { Field, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@pivox/primitives/select';
import { useMemo, useRef, useState } from 'react';

import { Grid, useGrid } from '../grid';

import { AdminFrame } from './admin-frame';
import { AdminSearch } from './admin-search';
import { AGENT_FILTER_ANY, AgentFilterSelect } from './agent-filter';
import { AgentSelect } from './agent-select';
import { ClearFiltersButton } from './clear-filters-button';
import {
  ConnectorsAdminContext,
  useConnectorsAdmin,
} from './connectors-admin.context';
import { DeleteDialog } from './delete-dialog';
import { FilterToggleButton } from './filter-toggle-button';
import { FormActions, FormDialog } from './form-dialog';
import { IdentifierField } from './identifier-field';
import { KeyValueEditor } from './key-value-editor';
import { actorLabel, formatTimestamp } from './meta-cells';
import { RowActions } from './row-actions';
import { ScopeSelect } from './scope-select';
import { isValidIdentifier, slugify } from './slug';

import type { GridColumn, GridContextValue } from '../grid';

import type { Suggestion } from './suggest-combobox';

import type {
  AgentOption,
  Connector,
  ConnectorFormValues,
  ConnectorsAdminContextValue,
  KeyValueEntry,
  SpaceOption,
} from './types';

/** Props every connector-type variant renders against. */
interface ConnectorFieldsProps {
  values: ConnectorFormValues;
  patch: (next: Partial<ConnectorFormValues>) => void;
  disabled: boolean;
  /** Portal mount for in-field combobox popups (inside the modal dialog). */
  container?: React.RefObject<HTMLElement | null>;
  /** Flip/shift boundary for those popups (the dialog element). */
  collisionBoundary?: Element | null;
}

/** Common HTTP request-header names offered in the Headers key combobox. */
const COMMON_HTTP_HEADERS: Suggestion[] = [
  { name: 'Authorization', description: 'Credentials for the target API' },
  { name: 'Content-Type', description: 'Media type of the request body' },
  { name: 'Accept', description: 'Media types the client will accept' },
  { name: 'Accept-Encoding', description: 'Content encodings the client accepts' },
  { name: 'Accept-Language', description: 'Preferred response languages' },
  { name: 'User-Agent', description: 'Client software identifier' },
  { name: 'X-Api-Key', description: 'API key credential' },
  { name: 'X-Request-Id', description: 'Correlation id for the request' },
  { name: 'Cache-Control', description: 'Caching directives' },
  { name: 'Cookie', description: 'Stored cookies to send' },
  { name: 'Origin', description: 'Origin of the request (CORS)' },
  { name: 'Referer', description: 'Address of the referring page' },
];

/**
 * HTTP variant fields (base URL + headers). Adding a future adapter = add a
 * sibling variant component + a `CONNECTOR_TYPES` option + a `CONNECTOR_FIELDS`
 * entry; nothing here changes.
 */
function HttpConnectorFields({
  values,
  patch,
  disabled,
  container,
  collisionBoundary,
}: ConnectorFieldsProps) {
  return (
    <>
      <Field>
        <FieldLabel>Base URL</FieldLabel>
        <Input
          value={values.baseUrl}
          onChange={(e) => patch({ baseUrl: e.target.value })}
          placeholder="https://api.example.com"
          disabled={disabled}
        />
      </Field>
      <KeyValueEditor
        label="Headers"
        description={
          <>
            Values are CEL over the connector config. Reference a secret with{' '}
            <code>secret(&quot;…/secrets/x&quot;)</code>, e.g.{' '}
            <code>&quot;Bearer &quot; + secret(&quot;…&quot;)</code>.
          </>
        }
        keyPlaceholder="Authorization"
        valuePlaceholder='secret("…/secrets/token")'
        entries={values.headers}
        onChange={(headers) => patch({ headers })}
        disabled={disabled}
        keySuggestions={COMMON_HTTP_HEADERS}
        keyContainer={container}
        keyBoundary={collisionBoundary}
      />
    </>
  );
}

/** The connector `config` oneof — HTTP is the only variant today. */
type ConnectorType = 'http';

const CONNECTOR_TYPES: { value: ConnectorType; label: string }[] = [
  { value: 'http', label: 'HTTP' },
];

const CONNECTOR_FIELDS: Record<
  ConnectorType,
  (props: ConnectorFieldsProps) => React.ReactNode
> = {
  http: HttpConnectorFields,
};

/** Per-type validators: the type-specific fields complete enough to submit. */
const CONNECTOR_CONFIG_VALID: Record<
  ConnectorType,
  (values: ConnectorFormValues) => boolean
> = {
  http: (values) => values.baseUrl.trim().length > 0,
};

function leafId(name: string | undefined): string {
  if (!name) return '';
  const segments = parseResourceName(name);
  return segments.connectors ?? '';
}

/** The connector's config-oneof case as a display label. Extend as cases land. */
function connectorType(connector: Connector): string | null {
  if (connector.http) return 'HTTP';
  return null;
}

/**
 * The agent column value: empty `agent` runs in the cloud; otherwise resolve the
 * agent resource name to its display label, falling back to the name leaf.
 */
function agentLabel(agent: string | undefined, options: AgentOption[]): string {
  if (!agent) return 'Cloud';
  const match = options.find((option) => option.value === agent);
  return match?.label ?? parseResourceName(agent).agents ?? agent;
}

/** The space slug of a connector name, or the empty string for an org-direct one. */
function connectorSpaceSlug(name: string | undefined): string {
  if (!name) return '';
  return parseResourceName(name).spaces ?? '';
}

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

/**
 * The space column value: a space-scoped connector shows its space (display name
 * if resolvable, else the slug); an org-direct connector shows "Organization".
 */
function spaceLabel(name: string | undefined, options: SpaceOption[]): string {
  const slug = connectorSpaceSlug(name);
  if (!slug) return 'Organization';
  const match = options.find((option) => option.slug === slug);
  return match?.displayName || slug;
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

function headersToEntries(
  headers: Record<string, string> | undefined,
): KeyValueEntry[] {
  return Object.entries(headers ?? {}).map(([key, value]) => ({ key, value }));
}

function ConnectorForm() {
  const { state, actions } = useConnectorsAdmin();
  const { dialog, agentOptions, spaceOptions } = state;
  const editing = dialog.editing;
  const isCreate = dialog.mode === 'create';

  const [values, setValues] = useState<ConnectorFormValues>(() => ({
    connectorId: '',
    displayName: editing?.displayName ?? '',
    description: editing?.description ?? '',
    baseUrl: editing?.http?.baseUrl ?? '',
    headers: headersToEntries(editing?.http?.headers),
    agent: editing?.agent ?? '',
    scope: connectorSpaceSlug(editing?.name),
  }));
  // The scope combobox portals its popup into this mount node so the dialog's
  // pointer-lock doesn't swallow mouse clicks on the options. The node is
  // absolutely positioned (out of flow), so Base UI's injected portal wrapper
  // does NOT become a flex child of the form — which would otherwise add a
  // `gap-4` (the observed +16px) and reflow the dialog.
  const popupMountRef = useRef<HTMLDivElement>(null);
  // Collision boundary for the in-dialog combobox popups: the form box (≈ the
  // dialog content, footer included). State-backed via a callback ref so the
  // resolved element is available even if a combobox opens before any re-render
  // — a bottom-of-dialog popup then flips ABOVE its input instead of overrunning
  // the footer (the default boundary is the viewport, where it still fits).
  const [dialogBoundary, setDialogBoundary] = useState<HTMLFormElement | null>(
    null,
  );
  // HTTP is the only config today; edit mode keeps the existing type.
  const [type, setType] = useState<ConnectorType>('http');
  // Until the user overrides the id, it auto-derives from the display name.
  const [idTouched, setIdTouched] = useState(false);
  const [editingId, setEditingId] = useState(false);

  const patch = (next: Partial<ConnectorFormValues>) => {
    setValues((v) => ({ ...v, ...next }));
  };

  const updateDisplayName = (displayName: string) => {
    setValues((v) => ({
      ...v,
      displayName,
      connectorId: idTouched ? v.connectorId : slugify(displayName),
    }));
  };

  const Fields = CONNECTOR_FIELDS[type];
  const canSubmit =
    CONNECTOR_CONFIG_VALID[type](values) &&
    (!isCreate || isValidIdentifier(values.connectorId));

  return (
    <form
      ref={setDialogBoundary}
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        actions.submit(values);
      }}
    >
      <Field>
        <FieldLabel>Display name</FieldLabel>
        <Input
          value={values.displayName}
          onChange={(e) => updateDisplayName(e.target.value)}
          disabled={dialog.pending}
        />
      </Field>
      {isCreate ? (
        <IdentifierField
          label="Identifier"
          value={values.connectorId}
          editing={editingId}
          onEdit={() => {
            setEditingId(true);
            setIdTouched(true);
          }}
          onChange={(connectorId) => {
            setIdTouched(true);
            patch({ connectorId });
          }}
          disabled={dialog.pending}
        />
      ) : null}
      <Field>
        <FieldLabel>Scope</FieldLabel>
        {isCreate ? (
          <ScopeSelect
            value={values.scope}
            spaces={spaceOptions}
            onChange={(scope) => patch({ scope })}
            allLabel="Organization (no space)"
            placeholder="No space — organization"
            disabled={dialog.pending}
            container={popupMountRef}
            collisionBoundary={dialogBoundary}
          />
        ) : (
          // Scope is immutable — a connector can't move between org and space.
          <Input value={spaceLabel(editing?.name, spaceOptions)} readOnly disabled />
        )}
      </Field>
      {/* Out-of-flow portal mount for the scope popup (see popupMountRef). */}
      <div ref={popupMountRef} className="absolute" />
      <Field>
        <FieldLabel>Type</FieldLabel>
        <Select
          value={type}
          onValueChange={(next) => {
            const match = CONNECTOR_TYPES.find((o) => o.value === next);
            if (match) setType(match.value);
          }}
          disabled={dialog.pending}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {CONNECTOR_TYPES.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <Fields
        values={values}
        patch={patch}
        disabled={dialog.pending}
        container={popupMountRef}
        collisionBoundary={dialogBoundary}
      />
      <Field>
        <FieldLabel>Run on Agent</FieldLabel>
        <AgentSelect
          value={values.agent}
          options={agentOptions}
          onChange={(agent) => patch({ agent })}
          disabled={dialog.pending}
          container={popupMountRef}
          collisionBoundary={dialogBoundary}
        />
      </Field>
      <FormActions
        error={dialog.error}
        pending={dialog.pending}
        canSubmit={canSubmit}
        submitLabel={isCreate ? 'Create connector' : 'Save changes'}
        onCancel={actions.closeDialog}
      />
    </form>
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
 * row (composition, not a boolean grid prop).
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
  const { dialog, remove, agentOptions, spaceOptions, filters, scope } = state;
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

      <FormDialog
        open={dialog.open}
        onOpenChange={(open) => {
          if (!open) actions.closeDialog();
        }}
        title={dialog.mode === 'create' ? 'New connector' : 'Edit connector'}
        description={
          dialog.mode === 'create'
            ? 'Point workflow activities at an external HTTP API.'
            : undefined
        }
      >
        <ConnectorForm key={dialog.editing?.name ?? 'new'} />
      </FormDialog>

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
