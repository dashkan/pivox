import { parseResourceName } from '@pivox/client';

import type { Suggestion } from './suggest-combobox';
import type {
  AgentOption,
  Connector,
  ConnectorFormValues,
  KeyValueEntry,
  SpaceOption,
} from './types';

/** The connector `config` oneof — HTTP is the only variant today. */
export type ConnectorType = 'http';

export const CONNECTOR_TYPES: { value: ConnectorType; label: string }[] = [
  { value: 'http', label: 'HTTP' },
];

/** Per-type validators: the type-specific fields complete enough to submit. */
export const CONNECTOR_CONFIG_VALID: Record<
  ConnectorType,
  (values: ConnectorFormValues) => boolean
> = {
  http: (values) => values.baseUrl.trim().length > 0,
};

/** Common HTTP request-header names offered in the Headers key combobox. */
export const COMMON_HTTP_HEADERS: Suggestion[] = [
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

/** The connector name's leaf id, or the empty string. */
export function leafId(name: string | undefined): string {
  if (!name) return '';
  return parseResourceName(name).connectors ?? '';
}

/** The space slug of a connector name, or the empty string for an org-direct one. */
export function connectorSpaceSlug(name: string | undefined): string {
  if (!name) return '';
  return parseResourceName(name).spaces ?? '';
}

/**
 * The space column / read-only-scope value: a space-scoped connector shows its
 * space (display name if resolvable, else the slug); an org-direct connector
 * shows "Organization".
 */
export function spaceLabel(
  name: string | undefined,
  options: SpaceOption[],
): string {
  const slug = connectorSpaceSlug(name);
  if (!slug) return 'Organization';
  const match = options.find((option) => option.slug === slug);
  return match?.displayName || slug;
}

/** The connector's config-oneof case as a display label. Extend as cases land. */
export function connectorType(connector: Connector): string | null {
  if (connector.http) return 'HTTP';
  return null;
}

/**
 * The agent column value: empty `agent` runs in the cloud; otherwise resolve the
 * agent resource name to its display label, falling back to the name leaf.
 */
export function agentLabel(
  agent: string | undefined,
  options: AgentOption[],
): string {
  if (!agent) return 'Cloud';
  const match = options.find((option) => option.value === agent);
  return match?.label ?? parseResourceName(agent).agents ?? agent;
}

/** Collapse a connector's header map into map-editor rows. */
export function headersToEntries(
  headers: Record<string, string> | undefined,
): KeyValueEntry[] {
  return Object.entries(headers ?? {}).map(([key, value]) => ({ key, value }));
}

/**
 * Seed connector form values from an edit record (or a blank create). Kept pure
 * so the form provider can `useState(() => seedConnectorValues(record))` (lazy
 * state init) and compare against it to derive `dirty` during render.
 */
export function seedConnectorValues(
  record: Connector | null,
): ConnectorFormValues {
  return {
    connectorId: '',
    displayName: record?.displayName ?? '',
    description: record?.description ?? '',
    baseUrl: record?.http?.baseUrl ?? '',
    headers: headersToEntries(record?.http?.headers),
    agent: record?.agent ?? '',
    scope: connectorSpaceSlug(record?.name),
  };
}
