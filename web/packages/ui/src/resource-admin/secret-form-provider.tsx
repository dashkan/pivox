'use client';

import { useCallback, useMemo, useState } from 'react';

import { FormPage } from '../form-page';

import { seedSecretValues } from './secret-shared';
import { SecretFormContext } from './secret-form.context';
import { isValidIdentifier } from './slug';

import type { Secret, SecretFormValues, SpaceOption } from './types';
import type { FormMode, FormPageContextValue } from '../form-page';
import type { ReactNode } from 'react';

/** Compare two secret form-value snapshots for the dirty check. */
function valuesEqual(a: SecretFormValues, b: SecretFormValues): boolean {
  if (
    a.secretId !== b.secretId ||
    a.displayName !== b.displayName ||
    a.value !== b.value ||
    a.rotate !== b.rotate ||
    a.scope !== b.scope ||
    a.annotations.length !== b.annotations.length
  ) {
    return false;
  }
  return a.annotations.every(
    (row, i) =>
      row.key === b.annotations[i]?.key && row.value === b.annotations[i]?.value,
  );
}

/**
 * Owns the secret form VALUES and maps them into the generic `FormPage`
 * contract — the secret twin of `ConnectorFormProvider`. Holds
 * `useState(() => seedSecretValues(record))` (lazy init, reset across edit
 * records by a keyed remount at the feature), DERIVES `canSubmit` + `dirty`
 * during render, builds `submit: () => mutate(values)`, and feeds the generic
 * contract. The mutation + navigation are INJECTED so this stays router- and
 * react-query-free.
 *
 * Set-only value: the value is required on create and whenever an edit rotates
 * it (`rotate`); a metadata-only edit leaves the stored value untouched.
 */
export function SecretFormProvider({
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
  children,
}: {
  mode: FormMode;
  /** Edit record to seed from; null in create. */
  record: Secret | null;
  recordLoading: boolean;
  loadError: string | null;
  /** A create/update write is in flight (feature-owned). */
  pending: boolean;
  /** Last failed-submit message (feature-owned), or null. */
  error: string | null;
  /** Commit the values: the feature runs the RPC + navigates on success. */
  mutate: (values: SecretFormValues) => void;
  onCancel: () => void;
  /** Edit-only: open the delete-confirm. Absent in create. */
  onDelete?: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  spaceOptions: SpaceOption[];
  children: ReactNode;
}) {
  const [values, setValues] = useState<SecretFormValues>(() =>
    seedSecretValues(record),
  );
  // Stable seed for this mount (remounted by key across edit records), so the
  // dirty check compares against the record the form opened on.
  const [initial] = useState<SecretFormValues>(() => seedSecretValues(record));

  const patch = useCallback((next: Partial<SecretFormValues>) => {
    setValues((current) => ({ ...current, ...next }));
  }, []);

  // Derived during render — never stored, never an effect. The value is required
  // on create and whenever an edit rotates it.
  const valueRequired = mode === 'create' || values.rotate;
  const canSubmit =
    (mode !== 'create' || isValidIdentifier(values.secretId)) &&
    (!valueRequired || values.value.length > 0);
  const dirty = !valuesEqual(values, initial);

  const formPageValue = useMemo<FormPageContextValue<Secret>>(
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
      meta: { resourceLabel: 'secret', onDirtyChange },
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

  const secretFormValue = useMemo(
    () => ({ mode, values, patch, spaceOptions, record }),
    [mode, values, patch, spaceOptions, record],
  );

  return (
    <FormPage.Provider value={formPageValue}>
      <SecretFormContext value={secretFormValue}>{children}</SecretFormContext>
    </FormPage.Provider>
  );
}
