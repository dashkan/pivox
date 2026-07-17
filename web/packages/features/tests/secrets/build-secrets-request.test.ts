import { createApiClient } from '@pivox/client';
import { createReactQueryApi } from '@pivox/client/react-query';
import { describe, expect, it } from 'vitest';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

import { buildSecretsListRequest } from '@/secrets/build-secrets-request';

const ORG_PATH = '/v1/organizations/{organization}/secrets' as const;
const SPACE_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets' as const;

const $api = createReactQueryApi(createApiClient({ baseUrl: '' }));

const base: ListControlsValue = {
  filters: {},
  sort: null,
  pageSize: 25,
  scope: '',
  pageToken: undefined,
};

describe('buildSecretsListRequest', () => {
  it('composes filter + order_by + page size + cursor into the query', () => {
    const req = buildSecretsListRequest('acme', {
      ...base,
      filters: { displayName: 'stripe' },
      sort: { field: 'createTime', direction: 'desc' },
      pageSize: 50,
      pageToken: 'tok',
    });
    expect(req.query).toEqual({
      filter: 'displayName:"stripe"',
      orderBy: 'createTime desc',
      pageSize: 50,
      pageToken: 'tok',
    });
  });

  it('targets the org rollup path params when scope is empty', () => {
    const req = buildSecretsListRequest('acme', base);
    expect(req.isSpaceScoped).toBe(false);
    expect(req.pathParams).toEqual({ organization: 'acme' });
  });

  it('targets the space path params when a scope is set', () => {
    const req = buildSecretsListRequest('acme', { ...base, scope: 'main' });
    expect(req.isSpaceScoped).toBe(true);
    expect(req.pathParams).toEqual({ organization: 'acme', space: 'main' });
  });
});

describe('buildSecretsListRequest — SSR/client query-key parity', () => {
  it('produces an identical org-scope query key from the same state', () => {
    const value: ListControlsValue = {
      ...base,
      filters: { displayName: 'stripe' },
      sort: { field: 'displayName', direction: 'asc' },
      pageSize: 50,
    };
    const client = buildSecretsListRequest('acme', value);
    const loader = buildSecretsListRequest('acme', value);

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
    const client = buildSecretsListRequest('acme', value);
    const loader = buildSecretsListRequest('acme', value);

    const clientKey = $api.queryOptions('get', SPACE_PATH, {
      params: { path: client.pathParams, query: client.query },
    }).queryKey;
    const loaderKey = $api.queryOptions('get', SPACE_PATH, {
      params: { path: loader.pathParams, query: loader.query },
    }).queryKey;

    expect(loaderKey).toEqual(clientKey);
  });
});
