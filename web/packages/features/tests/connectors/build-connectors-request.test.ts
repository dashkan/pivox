import { createApiClient } from '@pivox/client';
import { createReactQueryApi } from '@pivox/client/react-query';
import { AGENT_FILTER_CLOUD } from '@pivox/ui/resource-admin';
import { describe, expect, it } from 'vitest';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

import { buildConnectorsListRequest } from '@/connectors/build-connectors-request';

const ORG_PATH = '/v1/organizations/{organization}/connectors' as const;
const SPACE_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors' as const;

const $api = createReactQueryApi(createApiClient({ baseUrl: '' }));

const base: ListControlsValue = {
  filters: {},
  sort: null,
  pageSize: 25,
  scope: '',
  pageToken: undefined,
};

describe('buildConnectorsListRequest', () => {
  it('composes filter + order_by + page size + cursor into the query', () => {
    const req = buildConnectorsListRequest('acme', {
      ...base,
      filters: { displayName: 'stripe', agent: AGENT_FILTER_CLOUD },
      sort: { field: 'updateTime', direction: 'desc' },
      pageSize: 50,
      pageToken: 'tok',
    });
    expect(req.query).toEqual({
      filter: 'displayName:"stripe" AND agent=""',
      orderBy: 'updateTime desc',
      pageSize: 50,
      pageToken: 'tok',
    });
  });

  it('targets the org rollup path params when scope is empty', () => {
    const req = buildConnectorsListRequest('acme', base);
    expect(req.isSpaceScoped).toBe(false);
    expect(req.pathParams).toEqual({ organization: 'acme' });
  });

  it('targets the space path params when a scope is set', () => {
    const req = buildConnectorsListRequest('acme', { ...base, scope: 'main' });
    expect(req.isSpaceScoped).toBe(true);
    expect(req.pathParams).toEqual({ organization: 'acme', space: 'main' });
  });
});

describe('buildConnectorsListRequest — SSR/client query-key parity', () => {
  // The loader and the client hook both derive the request from the SAME URL
  // state via this builder, so their openapi-react-query keys must be identical.
  it('produces an identical org-scope query key from the same state', () => {
    const value: ListControlsValue = {
      ...base,
      filters: { displayName: 'stripe' },
      sort: { field: 'displayName', direction: 'asc' },
      pageSize: 50,
    };
    const client = buildConnectorsListRequest('acme', value);
    const loader = buildConnectorsListRequest('acme', value);

    const clientKey = $api.queryOptions('get', ORG_PATH, {
      params: { path: client.pathParams, query: client.query },
    }).queryKey;
    const loaderKey = $api.queryOptions('get', ORG_PATH, {
      params: { path: loader.pathParams, query: loader.query },
    }).queryKey;

    expect(loaderKey).toEqual(clientKey);
  });

  it('produces an identical space-scope query key from the same state', () => {
    const value: ListControlsValue = { ...base, scope: 'main' };
    const client = buildConnectorsListRequest('acme', value);
    const loader = buildConnectorsListRequest('acme', value);

    const clientKey = $api.queryOptions('get', SPACE_PATH, {
      params: { path: client.pathParams, query: client.query },
    }).queryKey;
    const loaderKey = $api.queryOptions('get', SPACE_PATH, {
      params: { path: loader.pathParams, query: loader.query },
    }).queryKey;

    expect(loaderKey).toEqual(clientKey);
  });
});
