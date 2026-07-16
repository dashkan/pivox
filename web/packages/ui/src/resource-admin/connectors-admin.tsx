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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@pivox/primitives/table';
import { useRef, useState } from 'react';

import { AdminFrame, AdminNoticeRow } from './admin-frame';
import { AdminPagination } from './admin-pagination';
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
import { SortableHeader } from './sortable-header';

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
      {isCreate && (
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
      )}
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

function ConnectorFilterRow() {
  const { state, actions } = useConnectorsAdmin();
  const { filters, agentOptions, agentsInUse, spaceOptions, scope } = state;

  // The agent facet lists only agents actually in scope, resolved to labels via
  // the full agent list (leaf fallback). Hidden entirely when every connector in
  // scope runs on the cloud (no agents in use).
  const inUseAgentOptions = agentsInUse.map((name) => ({
    value: name,
    label: agentLabel(name, agentOptions),
  }));

  return (
    <TableRow>
      <TableHead>
        <AdminSearch
          value={filters.displayName ?? ''}
          // Debounced text: replace history so keystrokes don't stack entries.
          onChange={(value) => actions.setFilter('displayName', value, 'replace')}
          placeholder="Filter by name"
          debounceMs={300}
        />
      </TableHead>
      <TableHead>
        <ScopeSelect
          value={scope}
          spaces={spaceOptions}
          onChange={actions.setScope}
          allLabel="All spaces"
        />
      </TableHead>
      <TableHead />
      <TableHead>
        {inUseAgentOptions.length > 0 && (
          <AgentFilterSelect
            value={filters.agent ?? AGENT_FILTER_ANY}
            // Discrete selection: push history so Back returns to the prior facet.
            onChange={(value) => actions.setFilter('agent', value, 'push')}
            options={inUseAgentOptions}
          />
        )}
      </TableHead>
      <TableHead />
      <TableHead className="w-0" />
    </TableRow>
  );
}

// One data column for each header (Name, Space, Type, Agent, Updated, actions).
const CONNECTOR_COLSPAN = 6;

/**
 * Body rows for the connectors table. Loading / error / empty each render a
 * single notice row so the header + filter row above never unmount (which would
 * drop focus from the filter inputs and hide the controls when there is no data).
 */
function ConnectorsTableBody() {
  const { state, actions } = useConnectorsAdmin();
  const { connectors, filters, scope, agentOptions, spaceOptions } = state;

  if (state.isLoading) {
    return (
      <AdminNoticeRow colSpan={CONNECTOR_COLSPAN}>
        Loading connectors…
      </AdminNoticeRow>
    );
  }
  if (state.loadError) {
    return (
      <AdminNoticeRow colSpan={CONNECTOR_COLSPAN}>
        {state.loadError}
      </AdminNoticeRow>
    );
  }
  if (connectors.length === 0) {
    return (
      <AdminNoticeRow colSpan={CONNECTOR_COLSPAN}>
        {hasActiveFilters(filters, scope)
          ? 'No connectors match your filters.'
          : 'No connectors yet.'}
      </AdminNoticeRow>
    );
  }

  return (
    <>
      {connectors.map((connector: Connector) => {
        const type = connectorType(connector);
        const spaceSlug = connectorSpaceSlug(connector.name);
        return (
          <TableRow key={connector.name}>
            <TableCell className="font-medium">
              <button
                type="button"
                className="text-left hover:underline"
                onClick={() => actions.openEdit(connector)}
              >
                {connector.displayName || leafId(connector.name)}
              </button>
            </TableCell>
            <TableCell className="text-muted-foreground">
              {spaceSlug ? spaceLabel(connector.name, spaceOptions) : ''}
            </TableCell>
            <TableCell>
              {type ? <Badge variant="secondary">{type}</Badge> : '—'}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {agentLabel(connector.agent, agentOptions)}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {formatTimestamp(connector.updateTime)} ·{' '}
              {actorLabel(connector.updatedBy)}
            </TableCell>
            <TableCell>
              <RowActions
                editLabel="Edit connector"
                removeLabel="Delete connector"
                onEdit={() => actions.openEdit(connector)}
                onRemove={() => actions.openRemove(connector)}
              />
            </TableCell>
          </TableRow>
        );
      })}
    </>
  );
}

function ConnectorsTable({ showFilters }: { showFilters: boolean }) {
  const { state, actions } = useConnectorsAdmin();
  const { sort } = state;

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <SortableHeader
            field="displayName"
            sort={sort}
            onToggle={actions.toggleSort}
          >
            Name
          </SortableHeader>
          <TableHead>Space</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>Agent</TableHead>
          <SortableHeader
            field="updateTime"
            sort={sort}
            onToggle={actions.toggleSort}
          >
            Updated
          </SortableHeader>
          <TableHead className="w-0" />
        </TableRow>
        {showFilters && <ConnectorFilterRow />}
      </TableHeader>
      <TableBody>
        <ConnectorsTableBody />
      </TableBody>
    </Table>
  );
}

function ConnectorsAdminRoot() {
  const { state, actions } = useConnectorsAdmin();
  const { dialog, remove, pageSize, pagination, filters, scope } = state;
  const [showFilters, setShowFilters] = useState(false);
  const filtersActive = hasActiveFilters(filters, scope);

  return (
    <>
      <AdminFrame
        title="Connectors"
        description="Reusable, credentialed connections to external systems, used by workflow activities."
        newLabel="New connector"
        onNew={actions.openCreate}
      >
        <div className="flex items-center gap-2">
          <FilterToggleButton
            active={showFilters}
            onToggle={() => setShowFilters((v) => !v)}
          />
          {filtersActive && (
            <ClearFiltersButton onClear={actions.clearFilters} />
          )}
        </div>
        <ConnectorsTable showFilters={showFilters} />
        <AdminPagination
          pageSize={pageSize}
          onPageSizeChange={actions.setPageSize}
          hasPrevPage={pagination.hasPrevPage}
          hasNextPage={pagination.hasNextPage}
          onPrev={actions.prevPage}
          onNext={actions.nextPage}
        />
      </AdminFrame>

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
