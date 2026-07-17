import { resourcePathParams } from '@/workflows/resource-paths';

import { buildConnectorBody } from './build-connector-body';

import type { ApiClient } from '@pivox/client';
import type { Connector, ConnectorFormValues } from '@pivox/ui/resource-admin';
import type { FormMode } from '@pivox/ui/form-page';

const CONNECTORS_PATH = '/v1/organizations/{organization}/connectors' as const;
const CONNECTOR_PATH =
  '/v1/organizations/{organization}/connectors/{connector}' as const;
const SPACE_CONNECTORS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors' as const;
const SPACE_CONNECTOR_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}' as const;

/**
 * Item-route params for a connector name. `space` is present when the name is
 * space-scoped (`organizations/*​/spaces/*​/connectors/*`), selecting the
 * space-scoped item path; absent selects the org-direct item path.
 */
export function connectorItemParams(name: string): {
  organization: string;
  space?: string;
  connector: string;
} {
  const p = resourcePathParams(name);
  return {
    organization: p.organization ?? '',
    space: p.space,
    connector: p.connector ?? '',
  };
}

/**
 * Creates or updates a connector on the path its scope dictates. Create targets
 * the org or the selected space (`values.scope`); update derives the scope from
 * the connector's name (edit can't move scope). Shared by the routed create/edit
 * form pages — `FormPage` never sees this RPC, per the design.
 */
export function saveConnector(input: {
  apiClient: ApiClient;
  mode: FormMode;
  editing: Connector | null;
  organization: string;
  values: ConnectorFormValues;
}) {
  const { apiClient, mode, editing, organization, values } = input;
  const body = buildConnectorBody(values);

  if (mode === 'create') {
    const query = { connectorId: values.connectorId };
    return values.scope
      ? apiClient.POST(SPACE_CONNECTORS_PATH, {
          params: { path: { organization, space: values.scope }, query },
          body,
        })
      : apiClient.POST(CONNECTORS_PATH, {
          params: { path: { organization }, query },
          body,
        });
  }

  const item = connectorItemParams(editing?.name ?? '');
  return item.space
    ? apiClient.PATCH(SPACE_CONNECTOR_PATH, {
        params: {
          path: {
            organization: item.organization,
            space: item.space,
            connector: item.connector,
          },
        },
        body,
      })
    : apiClient.PATCH(CONNECTOR_PATH, {
        params: {
          path: { organization: item.organization, connector: item.connector },
        },
        body,
      });
}

/** Deletes a connector by its full name, threading the optimistic-concurrency etag. */
export function deleteConnector(input: {
  apiClient: ApiClient;
  connector: Connector;
}) {
  const { apiClient, connector } = input;
  const item = connectorItemParams(connector.name ?? '');
  const etagQuery = connector.etag ? { etag: connector.etag } : {};
  return item.space
    ? apiClient.DELETE(SPACE_CONNECTOR_PATH, {
        params: {
          path: {
            organization: item.organization,
            space: item.space,
            connector: item.connector,
          },
          query: etagQuery,
        },
      })
    : apiClient.DELETE(CONNECTOR_PATH, {
        params: {
          path: { organization: item.organization, connector: item.connector },
          query: etagQuery,
        },
      });
}
