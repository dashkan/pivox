'use client';

import { useCallback, useMemo, useState } from 'react';

import { FormPage } from '../form-page';

import {
  CONNECTOR_CONFIG_VALID,
  seedConnectorValues,
} from './connector-shared';
import { ConnectorFormContext } from './connector-form.context';
import { isValidIdentifier } from './slug';

import type { ConnectorType } from './connector-shared';
import type {
  AgentOption,
  Connector,
  ConnectorFormValues,
  SpaceOption,
} from './types';
import type { FormMode, FormPageContextValue } from '../form-page';
import type { ReactNode } from 'react';

/** Compare two connector form-value snapshots for the dirty check. */
function valuesEqual(a: ConnectorFormValues, b: ConnectorFormValues): boolean {
  if (
    a.connectorId !== b.connectorId ||
    a.displayName !== b.displayName ||
    a.description !== b.description ||
    a.baseUrl !== b.baseUrl ||
    a.agent !== b.agent ||
    a.scope !== b.scope ||
    a.headers.length !== b.headers.length
  ) {
    return false;
  }
  return a.headers.every(
    (row, i) =>
      row.key === b.headers[i]?.key && row.value === b.headers[i]?.value,
  );
}

/**
 * Owns the connector form VALUES and maps them into the generic `FormPage`
 * contract — the single place the resource state becomes `FormPageContextValue`
 * (`state-decouple-implementation`). It:
 *
 *  - holds `useState(() => seedConnectorValues(record))` (lazy state init,
 *    `5.12`), reset across edit records by a keyed remount at the route/feature
 *    (values live HERE, so the key belongs on this provider, not the fields);
 *  - DERIVES `canSubmit` and `dirty` during render (`5.1 derive-during-render`),
 *    never an effect, never mirrored state;
 *  - builds `submit: () => mutate(values)` — the button in `FormPage.Submit` is
 *    a plain `<button type="submit">`; this provider already holds the values,
 *    so the generic contract stays free of connector types;
 *  - feeds `canSubmit / dirty / pending / error / submit / cancel / delete` into
 *    `FormPage.Provider`, and exposes `{ values, patch }` on its own context for
 *    the field components.
 *
 * The create/update mutation and navigation are INJECTED (from the feature) so
 * this stays router- and react-query-free.
 */
export function ConnectorFormProvider({
  mode,
  record,
  recordLoading,
  loadError,
  pending,
  error,
  mutate,
  onCancel,
  onDelete,
  onDirtyChange,
  spaceOptions,
  agentOptions,
  children,
}: {
  mode: FormMode;
  /** Edit record to seed from; null in create. */
  record: Connector | null;
  recordLoading: boolean;
  loadError: string | null;
  /** A create/update write is in flight (feature-owned). */
  pending: boolean;
  /** Last failed-submit message (feature-owned), or null. */
  error: string | null;
  /** Commit the values: the feature runs the RPC + navigates on success. */
  mutate: (values: ConnectorFormValues) => void;
  onCancel: () => void;
  /** Edit-only: open the delete-confirm. Absent in create. */
  onDelete?: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  spaceOptions: SpaceOption[];
  agentOptions: AgentOption[];
  children: ReactNode;
}) {
  const [values, setValues] = useState<ConnectorFormValues>(() =>
    seedConnectorValues(record),
  );
  // Stable seed for this mount (remounted by key across edit records), so the
  // dirty check compares against the record the form opened on.
  const [initial] = useState<ConnectorFormValues>(() =>
    seedConnectorValues(record),
  );
  // HTTP is the only config today; edit keeps the existing type.
  const [type, setType] = useState<ConnectorType>('http');

  const patch = useCallback((next: Partial<ConnectorFormValues>) => {
    setValues((current) => ({ ...current, ...next }));
  }, []);

  // Derived during render — never stored, never an effect.
  const canSubmit =
    CONNECTOR_CONFIG_VALID[type](values) &&
    (mode !== 'create' || isValidIdentifier(values.connectorId));
  const dirty = !valuesEqual(values, initial);

  const formPageValue = useMemo<FormPageContextValue<Connector>>(
    () => ({
      state: {
        mode,
        pending,
        error,
        canSubmit,
        dirty,
        record,
        recordLoading,
        loadError,
      },
      actions: {
        submit: () => mutate(values),
        cancel: onCancel,
        delete: onDelete,
      },
      meta: { resourceLabel: 'connector', onDirtyChange },
    }),
    [
      mode,
      pending,
      error,
      canSubmit,
      dirty,
      record,
      recordLoading,
      loadError,
      mutate,
      values,
      onCancel,
      onDelete,
      onDirtyChange,
    ],
  );

  const connectorFormValue = useMemo(
    () => ({
      mode,
      values,
      patch,
      type,
      setType,
      spaceOptions,
      agentOptions,
      record,
    }),
    [mode, values, patch, type, spaceOptions, agentOptions, record],
  );

  return (
    <FormPage.Provider value={formPageValue}>
      <ConnectorFormContext value={connectorFormValue}>
        {children}
      </ConnectorFormContext>
    </FormPage.Provider>
  );
}
