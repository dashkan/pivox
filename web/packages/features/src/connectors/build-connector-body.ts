import type { components } from '@pivox/client/types';
import type { ConnectorFormValues, KeyValueEntry } from '@pivox/ui/resource-admin';

type ConnectorBody = components['schemas']['v1Connector'];

/** Collapse map-editor rows into a header map, dropping blank-key rows. */
export function entriesToMap(entries: KeyValueEntry[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const { key, value } of entries) {
    const trimmed = key.trim();
    if (trimmed) out[trimmed] = value;
  }
  return out;
}

/**
 * Connector body for Create and Update. Both accept the same field subset; the
 * server derives `name` from the `connectorId` query param on create and from
 * the path on update, so it is never sent here.
 */
export function buildConnectorBody(values: ConnectorFormValues): ConnectorBody {
  // `headers` is ALWAYS set (even `{}`). The REST gateway derives the PATCH
  // field mask from body-field presence, so omitting it on an N→0 clear would
  // leave the stored headers in place — a de-scoping edit (removing an
  // `Authorization: secret("…")` row) must actually clear them server-side.
  const http: components['schemas']['v1HttpConnector'] = {
    baseUrl: values.baseUrl.trim(),
    headers: entriesToMap(values.headers),
  };

  return {
    displayName: values.displayName,
    description: values.description,
    http,
    agent: values.agent.trim(),
  };
}
