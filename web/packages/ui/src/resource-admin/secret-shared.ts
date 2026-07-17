import { parseResourceName } from '@pivox/client';

import type { KeyValueEntry, Secret, SecretFormValues } from './types';

/** The secret name's leaf id, or the empty string. */
export function secretLeafId(name: string | undefined): string {
  if (!name) return '';
  return parseResourceName(name).secrets ?? '';
}

/** The space slug of a secret name, or the empty string for an org-direct one. */
export function secretSpaceSlug(name: string | undefined): string {
  if (!name) return '';
  return parseResourceName(name).spaces ?? '';
}

/** Collapse a secret's annotation map into map-editor rows. */
export function annotationsToEntries(
  annotations: Record<string, string> | undefined,
): KeyValueEntry[] {
  return Object.entries(annotations ?? {}).map(([key, value]) => ({
    key,
    value,
  }));
}

/**
 * Seed secret form values from an edit record (or a blank create). Kept pure so
 * the form provider can `useState(() => seedSecretValues(record))` (lazy state
 * init) and compare against it to derive `dirty` during render. The `value` is
 * write-only and never returned by the API, so it always seeds empty; `rotate`
 * defaults on for create (the value is always written) and off for edit.
 */
export function seedSecretValues(record: Secret | null): SecretFormValues {
  return {
    secretId: '',
    displayName: record?.displayName ?? '',
    annotations: annotationsToEntries(record?.annotations),
    value: '',
    rotate: record === null,
    scope: secretSpaceSlug(record?.name),
  };
}
