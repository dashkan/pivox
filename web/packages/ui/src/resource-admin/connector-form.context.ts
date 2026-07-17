'use client';

import { createContext, use } from 'react';

import type { ConnectorType } from './connector-shared';
import type {
  AgentOption,
  Connector,
  ConnectorFormValues,
  SpaceOption,
} from './types';
import type { FormMode } from '../form-page';

/**
 * The connectors form's OWN context — the second, resource-owned context that
 * carries the form `values` the generic `FormPage` contract deliberately omits.
 * Connectors already runs two contexts side by side (the generic `Grid` context
 * plus `ConnectorsAdminContext`); this is the same layering for the form:
 * `FormPage` provides `{ state, actions, meta }` (canSubmit / pending / submit),
 * while THIS provides `{ values, patch, … }`. The field components read this;
 * `FormPage.Submit` reads the generic one. Neither knows the other's shape.
 */
export interface ConnectorFormContextValue {
  mode: FormMode;
  values: ConnectorFormValues;
  /** Functional-merge patch (`5.11 functional-setState`), stable across renders. */
  patch: (next: Partial<ConnectorFormValues>) => void;
  /** The selected config-oneof case (HTTP only today). */
  type: ConnectorType;
  setType: (type: ConnectorType) => void;
  /** Spaces to create into (create-fields scope picker); consumer-injected. */
  spaceOptions: SpaceOption[];
  /** Assignable agents for the "Run on Agent" field; consumer-injected. */
  agentOptions: AgentOption[];
  /** The edit record (for the read-only scope label); null in create. */
  record: Connector | null;
}

export const ConnectorFormContext =
  createContext<ConnectorFormContextValue | null>(null);

export function useConnectorForm(): ConnectorFormContextValue {
  const ctx = use(ConnectorFormContext);
  if (!ctx) {
    throw new Error(
      'Connector form fields must be used within a <ConnectorFormProvider>.',
    );
  }
  return ctx;
}
