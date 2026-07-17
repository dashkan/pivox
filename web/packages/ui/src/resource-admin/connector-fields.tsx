'use client';

import { Field, FieldLabel } from '@pivox/primitives/field';
import { Input } from '@pivox/primitives/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@pivox/primitives/select';
import { useState } from 'react';

import { AgentSelect } from './agent-select';
import {
  COMMON_HTTP_HEADERS,
  CONNECTOR_TYPES,
  spaceLabel,
} from './connector-shared';
import { useConnectorForm } from './connector-form.context';
import { IdentifierField } from './identifier-field';
import { KeyValueEditor } from './key-value-editor';
import { ScopeSelect } from './scope-select';
import { slugify } from './slug';

/**
 * Connector fields split into EXPLICIT create/edit variants
 * (`patterns-explicit-variants`) — the `isCreate` ladder that today's
 * `ConnectorForm` branches on inline (identifier on create only, scope editable
 * on create / read-only on edit) disappears. Shared trailing fields extract to
 * `ConnectorConfigFields`, which both variants compose. All components are
 * module-level (`5.4 no-component-in-component`).
 *
 * These read the resource-owned `ConnectorFormContext` for `values` / `patch`;
 * `FormPage.Submit` reads the generic context for `canSubmit`. Neither knows the
 * other's shape. No portal/collision-boundary props anymore — the routed page
 * killed the modal-combobox bug class the old form fought with `popupMountRef`.
 */

/** HTTP variant fields (base URL + headers). */
function HttpConnectorFields() {
  const { values, patch } = useConnectorForm();
  return (
    <>
      <Field>
        <FieldLabel>Base URL</FieldLabel>
        <Input
          value={values.baseUrl}
          onChange={(e) => patch({ baseUrl: e.target.value })}
          placeholder="https://api.example.com"
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
        keySuggestions={COMMON_HTTP_HEADERS}
      />
    </>
  );
}

/** The connector type + variant fields + "Run on Agent", shared by both variants. */
function ConnectorConfigFields() {
  const { values, patch, type, setType, agentOptions } = useConnectorForm();
  return (
    <>
      <Field>
        <FieldLabel>Type</FieldLabel>
        <Select
          value={type}
          onValueChange={(next) => {
            const match = CONNECTOR_TYPES.find((o) => o.value === next);
            if (match) setType(match.value);
          }}
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
      {/* HTTP is the only variant today; a future adapter adds a sibling. */}
      <HttpConnectorFields />
      <Field>
        <FieldLabel>Run on Agent</FieldLabel>
        <AgentSelect
          value={values.agent}
          options={agentOptions}
          onChange={(agent) => patch({ agent })}
        />
      </Field>
    </>
  );
}

/**
 * Create field-set: editable display name (auto-derives the identifier until the
 * user overrides it), the immutable identifier, an editable scope PICKER (org
 * rollup — the parent isn't decided yet), then the shared config fields. Scope
 * is just a value in the resource form context, so `submit()` reads it the same
 * whether it came from this picker or (future) a route param — the shell never
 * learns it existed.
 */
export function ConnectorCreateFields() {
  const { values, patch, spaceOptions } = useConnectorForm();
  // Local UI toggles: until the user overrides the id, it tracks the display
  // name; once editing, the parent stops re-deriving.
  const [idTouched, setIdTouched] = useState(false);
  const [editingId, setEditingId] = useState(false);

  const updateDisplayName = (displayName: string) => {
    patch({
      displayName,
      connectorId: idTouched ? values.connectorId : slugify(displayName),
    });
  };

  return (
    <>
      <Field>
        <FieldLabel>Display name</FieldLabel>
        <Input
          value={values.displayName}
          onChange={(e) => updateDisplayName(e.target.value)}
        />
      </Field>
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
      />
      <Field>
        <FieldLabel>Scope</FieldLabel>
        <ScopeSelect
          value={values.scope}
          spaces={spaceOptions}
          onChange={(scope) => patch({ scope })}
          allLabel="Organization (no space)"
          placeholder="No space — organization"
        />
      </Field>
      <ConnectorConfigFields />
    </>
  );
}

/**
 * Edit field-set: plain display name (no identifier to derive), the immutable
 * scope shown read-only (a connector can't move between org and space), then the
 * shared config fields. No identifier field — the identifier is permanent.
 */
export function ConnectorEditFields() {
  const { values, patch, record, spaceOptions } = useConnectorForm();
  return (
    <>
      <Field>
        <FieldLabel>Display name</FieldLabel>
        <Input
          value={values.displayName}
          onChange={(e) => patch({ displayName: e.target.value })}
        />
      </Field>
      <Field>
        <FieldLabel>Scope</FieldLabel>
        {/* Immutable — a connector can't move between org and space. */}
        <Input value={spaceLabel(record?.name, spaceOptions)} readOnly disabled />
      </Field>
      <ConnectorConfigFields />
    </>
  );
}
