// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { AGENT_FILTER_CLOUD } from '@pivox/ui/resource-admin';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { ListControlsValue } from '@pivox/ui/resource-admin';

import { useConnectors } from '@/connectors/use-connectors';

const ORG_LIST = '/v1/organizations/{organization}/connectors';
const SPACE_LIST = '/v1/organizations/{organization}/spaces/{space}/connectors';

interface QueryInit {
  params?: { path?: unknown; query?: Record<string, unknown> };
}
type QueryOptions = { enabled?: boolean };
type QueryResult = { data?: unknown; isLoading?: boolean; error?: unknown };

function makeApi(response: QueryResult) {
  const calls: { path: string; init: QueryInit; options?: QueryOptions }[] = [];
  const useQuery = (
    _method: string,
    path: string,
    init: QueryInit,
    options?: QueryOptions,
  ) => {
    calls.push({ path, init, options });
    if (options && options.enabled === false) {
      return { data: undefined, isLoading: false, error: undefined, refetch: vi.fn() };
    }
    return { ...response, refetch: vi.fn() };
  };
  const activeList = () =>
    calls
      .filter(
        (c) => c.path.endsWith('/connectors') && (c.options?.enabled ?? true),
      )
      .at(-1);
  return {
    $api: { useQuery } as unknown as ReactQueryApi,
    listQuery: () => activeList()?.init.params?.query,
    listPath: () => activeList()?.path,
  };
}

function makeApiClient() {
  const GET = vi.fn((_path: string, _init?: unknown) =>
    Promise.resolve({ data: { storageGateways: [] } }),
  );
  const POST = vi.fn((_path: string, _init: unknown) =>
    Promise.resolve({ error: undefined }),
  );
  const PATCH = vi.fn((_path: string, _init: unknown) =>
    Promise.resolve({ error: undefined }),
  );
  const DELETE = vi.fn((_path: string, _init: unknown) =>
    Promise.resolve({ error: undefined }),
  );
  return {
    apiClient: { GET, POST, PATCH, DELETE } as unknown as ApiClient,
    GET,
    POST,
  };
}

const parent = 'organizations/acme';

const baseState: ListControlsValue = {
  filters: {},
  sort: null,
  pageSize: 25,
  scope: '',
  pageToken: undefined,
};

function renderConnectors(
  listState: ListControlsValue,
  response: QueryResult = { data: { connectors: [] } },
) {
  const onListStateChange = vi.fn();
  const api = makeApi(response);
  const client = makeApiClient();
  const onCreate = vi.fn();
  const onEdit = vi.fn();
  const { result } = renderHook(() =>
    useConnectors({
      $api: api.$api,
      apiClient: client.apiClient,
      parent,
      listState,
      onListStateChange,
      agentOptions: [],
      onCreate,
      onEdit,
    }),
  );
  return {
    result,
    onListStateChange,
    onCreate,
    onEdit,
    ...api,
    GET: client.GET,
    POST: client.POST,
  };
}

async function settle() {
  await act(async () => {});
}

describe('useConnectors — query reflects the route-owned state', () => {
  it('builds a substring name filter from the state', async () => {
    const { listQuery } = renderConnectors({
      ...baseState,
      filters: { displayName: 'stripe' },
    });
    await settle();
    expect(listQuery()?.filter).toBe('displayName:"stripe"');
  });

  it('ANDs a name and agent filter', async () => {
    const { listQuery } = renderConnectors({
      ...baseState,
      filters: { displayName: 'stripe', agent: AGENT_FILTER_CLOUD },
    });
    await settle();
    expect(listQuery()?.filter).toBe('displayName:"stripe" AND agent=""');
  });

  it('maps sort to order_by and threads page size', async () => {
    const { listQuery } = renderConnectors({
      ...baseState,
      sort: { field: 'updateTime', direction: 'desc' },
      pageSize: 50,
    });
    await settle();
    expect(listQuery()?.orderBy).toBe('updateTime desc');
    expect(listQuery()?.pageSize).toBe(50);
  });

  it('targets the space list path when scope is set', async () => {
    const { listPath } = renderConnectors({ ...baseState, scope: 'main' });
    await settle();
    expect(listPath()).toBe(SPACE_LIST);
  });

  it('targets the org rollup path by default', async () => {
    const { listPath } = renderConnectors(baseState);
    await settle();
    expect(listPath()).toBe(ORG_LIST);
  });

  it('fires NO gateways/agents fan-out request (agents are injected)', async () => {
    const { GET } = renderConnectors(baseState);
    await settle();
    // The removed fan-out was the only apiClient.GET on this page.
    expect(GET).not.toHaveBeenCalled();
  });

  it('exposes agentsInUse from the list response for the filter facet', async () => {
    const agent = 'organizations/acme/storageGateways/gw/agents/a1';
    const { result } = renderConnectors(baseState, {
      data: { connectors: [], agentsInUse: [agent] },
    });
    await settle();
    expect(result.current.state.extras.agentsInUse).toEqual([agent]);
  });
});

describe('useConnectors — actions commit through onListStateChange', () => {
  it('setFilter merges the field and clears the cursor', async () => {
    const { result, onListStateChange } = renderConnectors({
      ...baseState,
      pageToken: 'tok',
    });
    await settle();
    act(() => result.current.actions.setFilter('agent', AGENT_FILTER_CLOUD));
    expect(onListStateChange).toHaveBeenCalledWith(
      expect.objectContaining({
        filters: { agent: AGENT_FILTER_CLOUD },
        pageToken: undefined,
      }),
      { history: 'push' },
    );
  });

  it('setScope switches scope and clears the cursor', async () => {
    const { result, onListStateChange } = renderConnectors({
      ...baseState,
      pageToken: 'tok',
    });
    await settle();
    act(() => result.current.actions.setScope('main'));
    expect(onListStateChange).toHaveBeenCalledWith(
      expect.objectContaining({ scope: 'main', pageToken: undefined }),
      { history: 'push' },
    );
  });

  it('toggleSort clears the cursor', async () => {
    const { result, onListStateChange } = renderConnectors({
      ...baseState,
      pageToken: 'tok',
    });
    await settle();
    act(() => result.current.actions.toggleSort('displayName'));
    expect(onListStateChange).toHaveBeenCalledWith(
      expect.objectContaining({
        sort: { field: 'displayName', direction: 'asc' },
        pageToken: undefined,
      }),
      { history: 'push' },
    );
  });

  it('clearFilters resets filters + scope + cursor', async () => {
    const { result, onListStateChange } = renderConnectors({
      ...baseState,
      filters: { displayName: 'x' },
      scope: 'main',
      pageToken: 'tok',
    });
    await settle();
    act(() => result.current.actions.clearFilters());
    expect(onListStateChange).toHaveBeenCalledWith(
      expect.objectContaining({ filters: {}, scope: '', pageToken: undefined }),
      { history: 'push' },
    );
  });

  it('nextPage advances the cursor to the response token', async () => {
    const { result, onListStateChange } = renderConnectors(baseState, {
      data: { connectors: [], nextPageToken: 'tok' },
    });
    await settle();
    expect(result.current.state.pagination.hasNextPage).toBe(true);
    act(() => result.current.actions.nextPage());
    expect(onListStateChange).toHaveBeenCalledWith(
      expect.objectContaining({ pageToken: 'tok' }),
      { history: 'push' },
    );
  });
});

describe('useConnectors — routed create/edit navigation', () => {
  // Create/edit are routed pages now: the list delegates to the route-injected
  // navigation callbacks (the create-scope RPC moved to `saveConnector`, tested
  // in save-connector.test.ts).
  it('openCreate delegates to the injected onCreate navigation', async () => {
    const { result, onCreate } = renderConnectors(baseState);
    await settle();
    act(() => result.current.actions.openCreate());
    expect(onCreate).toHaveBeenCalledTimes(1);
  });

  it('openEdit delegates to the injected onEdit navigation with the connector', async () => {
    const { result, onEdit } = renderConnectors(baseState);
    await settle();
    const connector = { name: 'organizations/acme/connectors/stripe' };
    act(() => result.current.actions.openEdit(connector));
    expect(onEdit).toHaveBeenCalledWith(connector);
  });
});
