'use client';

import { createContext, use } from 'react';

import type { Secret, SecretFormValues, SpaceOption } from './types';
import type { FormMode } from '../form-page';

/**
 * The secrets form's OWN context — the resource-owned context that carries the
 * form `values` the generic `FormPage` contract deliberately omits (the same
 * layering connectors uses). The field components read this; `FormPage.Submit`
 * reads the generic `FormPage` context. Neither knows the other's shape.
 */
export interface SecretFormContextValue {
  mode: FormMode;
  values: SecretFormValues;
  /** Functional-merge patch, stable across renders. */
  patch: (next: Partial<SecretFormValues>) => void;
  /** Spaces to create into (create-fields scope picker); consumer-injected. */
  spaceOptions: SpaceOption[];
  /** The edit record (for the read-only scope label); null in create. */
  record: Secret | null;
}

export const SecretFormContext = createContext<SecretFormContextValue | null>(
  null,
);

export function useSecretForm(): SecretFormContextValue {
  const ctx = use(SecretFormContext);
  if (!ctx) {
    throw new Error(
      'Secret form fields must be used within a <SecretFormProvider>.',
    );
  }
  return ctx;
}
