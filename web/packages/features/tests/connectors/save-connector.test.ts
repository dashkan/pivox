import { describe, expect, it, vi } from 'vitest';

import { deleteConnector, saveConnector } from '@/connectors/save-connector';

import type { ApiClient } from '@pivox/client';
import type { Connector, ConnectorFormValues } from '@pivox/ui/resource-admin';

const ORG_LIST = '/v1/organizations/{organization}/connectors';
const SPACE_LIST = '/v1/organizations/{organization}/spaces/{space}/connectors';
const ORG_ITEM = '/v1/organizations/{organization}/connectors/{connector}';
const SPACE_ITEM =
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}';

interface Call {
  path: string;
  init: { params: { path: Record<string, string>; query?: Record<string, unknown> } };
}

function makeClient() {
  const calls: Record<'POST' | 'PATCH' | 'DELETE', Call[]> = {
    POST: [],
    PATCH: [],
    DELETE: [],
  };
  const record =
    (kind: 'POST' | 'PATCH' | 'DELETE') =>
    (path: string, init: Call['init']) => {
      calls[kind].push({ path, init });
      return Promise.resolve({ error: undefined });
    };
  return {
    apiClient: {
      POST: vi.fn(record('POST')),
      PATCH: vi.fn(record('PATCH')),
      DELETE: vi.fn(record('DELETE')),
    } as unknown as ApiClient,
    calls,
  };
}

const values: ConnectorFormValues = {
  connectorId: 'stripe',
  displayName: 'Stripe',
  description: '',
  baseUrl: 'https://api.stripe.com',
  headers: [],
  agent: '',
  scope: '',
};

describe('saveConnector — create scope', () => {
  it('POSTs the org rollup path when scope is empty', () => {
    const { apiClient, calls } = makeClient();
    void saveConnector({
      apiClient,
      mode: 'create',
      editing: null,
      organization: 'acme',
      values: { ...values, scope: '' },
    });
    expect(calls.POST).toHaveLength(1);
    expect(calls.POST[0]?.path).toBe(ORG_LIST);
    expect(calls.POST[0]?.init.params.path.space).toBeUndefined();
    expect(calls.POST[0]?.init.params.query?.connectorId).toBe('stripe');
  });

  it('POSTs the space path when a space scope is selected', () => {
    const { apiClient, calls } = makeClient();
    void saveConnector({
      apiClient,
      mode: 'create',
      editing: null,
      organization: 'acme',
      values: { ...values, scope: 'main' },
    });
    expect(calls.POST).toHaveLength(1);
    expect(calls.POST[0]?.path).toBe(SPACE_LIST);
    expect(calls.POST[0]?.init.params.path.space).toBe('main');
  });
});

describe('saveConnector — update derives scope from the name', () => {
  it('PATCHes the org-direct item path', () => {
    const { apiClient, calls } = makeClient();
    const editing: Connector = {
      name: 'organizations/acme/connectors/stripe',
    };
    void saveConnector({
      apiClient,
      mode: 'edit',
      editing,
      organization: 'acme',
      values,
    });
    expect(calls.PATCH).toHaveLength(1);
    expect(calls.PATCH[0]?.path).toBe(ORG_ITEM);
    expect(calls.PATCH[0]?.init.params.path.space).toBeUndefined();
  });

  it('PATCHes the space item path for a space-scoped connector', () => {
    const { apiClient, calls } = makeClient();
    const editing: Connector = {
      name: 'organizations/acme/spaces/main/connectors/vizrt',
    };
    void saveConnector({
      apiClient,
      mode: 'edit',
      editing,
      organization: 'acme',
      values,
    });
    expect(calls.PATCH).toHaveLength(1);
    expect(calls.PATCH[0]?.path).toBe(SPACE_ITEM);
    expect(calls.PATCH[0]?.init.params.path.space).toBe('main');
  });
});

describe('deleteConnector — path + etag', () => {
  it('DELETEs the org-direct path, threading the etag', () => {
    const { apiClient, calls } = makeClient();
    void deleteConnector({
      apiClient,
      connector: { name: 'organizations/acme/connectors/stripe', etag: 'v1' },
    });
    expect(calls.DELETE).toHaveLength(1);
    expect(calls.DELETE[0]?.path).toBe(ORG_ITEM);
    expect(calls.DELETE[0]?.init.params.query?.etag).toBe('v1');
  });

  it('DELETEs the space path for a space-scoped connector', () => {
    const { apiClient, calls } = makeClient();
    void deleteConnector({
      apiClient,
      connector: { name: 'organizations/acme/spaces/main/connectors/vizrt' },
    });
    expect(calls.DELETE).toHaveLength(1);
    expect(calls.DELETE[0]?.path).toBe(SPACE_ITEM);
    expect(calls.DELETE[0]?.init.params.path.space).toBe('main');
  });
});
