// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { useResourceList } from '@/resource-admin/use-resource-list';

import type { ListDescriptor, ListQueryState } from '@/resource-admin/list-descriptor';
import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  ListControlsValue,
  ResourceListView,
} from '@pivox/ui/resource-admin';

/**
 * Proves the generic, descriptor-driven list hook is resource-AGNOSTIC: it drives
 * an arbitrary `Widget` resource (not connectors) through the same machinery —
 * row/extras extraction, list-controls delegation, pagination, and the row-delete
 * confirm — using only the descriptor's pure methods. Nothing here mentions a
 * connector, which is the point: a new admin resource is one descriptor.
 */

interface Widget {
  id: string;
}
interface WidgetExtras {
  injectedTags: string[];
  serverCount: number;
}
interface WidgetInjected {
  tags: string[];
}

const view = {} as ResourceListView<Widget, WidgetExtras>;

/** A controllable list-query result the fake descriptor returns. */
let queryResult: ListQueryState;
const refetch = vi.fn(() => Promise.resolve(undefined));
const removeSpy = vi.fn(() => Promise.resolve({ error: undefined }));
let lastUseListState: ListControlsValue | null = null;

function makeDescriptor(): ListDescriptor<Widget, WidgetExtras, WidgetInjected> {
  return {
    key: 'widgets',
    useList: ({ state }) => {
      lastUseListState = state;
      return queryResult;
    },
    rowsOf: (data) => (data as { widgets?: Widget[] } | undefined)?.widgets ?? [],
    nextPageTokenOf: (data) =>
      (data as { nextPageToken?: string } | undefined)?.nextPageToken,
    rowId: (w) => w.id,
    extrasOf: (data, injected) => ({
      injectedTags: injected.tags,
      serverCount: (data as { count?: number } | undefined)?.count ?? 0,
    }),
    remove: removeSpy,
    loadErrorFallback: 'widgets load failed',
    view,
  };
}

const baseState: ListControlsValue = {
  filters: {},
  sort: null,
  pageSize: 25,
  scope: '',
  pageToken: undefined,
};

function render(opts: {
  state?: Partial<ListControlsValue>;
  result?: Partial<ListQueryState>;
  injected?: WidgetInjected;
}) {
  queryResult = {
    data: undefined,
    isLoading: false,
    error: undefined,
    refetch,
    ...opts.result,
  };
  const onStateChange = vi.fn();
  const onCreate = vi.fn();
  const onEdit = vi.fn();
  const apiClient = {} as ApiClient;
  const $api = {} as ReactQueryApi;
  const descriptor = makeDescriptor();
  const { result } = renderHook(() =>
    useResourceList(descriptor, {
      $api,
      apiClient,
      parent: 'organizations/acme',
      state: { ...baseState, ...opts.state },
      onStateChange,
      injected: opts.injected ?? { tags: [] },
      onCreate,
      onEdit,
    }),
  );
  return { result, onStateChange, onCreate, onEdit, apiClient };
}

async function settle() {
  await act(async () => {});
}

describe('useResourceList — descriptor-driven extraction', () => {
  it('extracts rows via the descriptor rowsOf', () => {
    const { result } = render({
      result: { data: { widgets: [{ id: 'a' }, { id: 'b' }] } },
    });
    expect(result.current.state.rows).toEqual([{ id: 'a' }, { id: 'b' }]);
  });

  it('assembles extras from injected + per-response data', () => {
    const { result } = render({
      result: { data: { count: 7 } },
      injected: { tags: ['x', 'y'] },
    });
    expect(result.current.state.extras).toEqual({
      injectedTags: ['x', 'y'],
      serverCount: 7,
    });
  });

  it('surfaces a load error through the descriptor fallback', () => {
    const { result } = render({ result: { error: { code: 2, message: '' } } });
    expect(result.current.state.loadError).toBe('widgets load failed');
  });

  it('prefers the server error message when present', () => {
    const { result } = render({
      result: { error: { code: 2, message: 'boom' } },
    });
    expect(result.current.state.loadError).toBe('boom');
  });

  it('passes the URL-owned state through to the descriptor query', () => {
    render({ state: { scope: 'main', filters: { q: 'z' } } });
    expect(lastUseListState).toMatchObject({ scope: 'main', filters: { q: 'z' } });
  });
});

describe('useResourceList — pagination', () => {
  it('reports hasNextPage from the response cursor and advances on nextPage', () => {
    const { result, onStateChange } = render({
      result: { data: { widgets: [], nextPageToken: 'tok' } },
    });
    expect(result.current.state.pagination.hasNextPage).toBe(true);
    act(() => result.current.actions.nextPage());
    expect(onStateChange).toHaveBeenCalledWith(
      expect.objectContaining({ pageToken: 'tok' }),
      { history: 'push' },
    );
  });

  it('does not advance when there is no next cursor', () => {
    const { result, onStateChange } = render({ result: { data: { widgets: [] } } });
    expect(result.current.state.pagination.hasNextPage).toBe(false);
    act(() => result.current.actions.nextPage());
    expect(onStateChange).not.toHaveBeenCalled();
  });
});

describe('useResourceList — controls + navigation delegation', () => {
  it('delegates create/edit to the injected callbacks', () => {
    const { result, onCreate, onEdit } = render({});
    act(() => result.current.actions.openCreate());
    expect(onCreate).toHaveBeenCalledTimes(1);
    act(() => result.current.actions.openEdit({ id: 'a' }));
    expect(onEdit).toHaveBeenCalledWith({ id: 'a' });
  });

  it('commits a filter change through onStateChange (cursor reset)', () => {
    const { result, onStateChange } = render({ state: { pageToken: 'p' } });
    act(() => result.current.actions.setFilter('q', 'hi'));
    expect(onStateChange).toHaveBeenCalledWith(
      expect.objectContaining({ filters: { q: 'hi' }, pageToken: undefined }),
      { history: 'push' },
    );
  });
});

describe('useResourceList — row-delete confirm', () => {
  it('opens, confirms, deletes via the descriptor, then clears + refetches', async () => {
    removeSpy.mockClear();
    refetch.mockClear();
    const { result, apiClient } = render({});
    act(() => result.current.actions.openRemove({ id: 'a' }));
    expect(result.current.state.remove.target).toEqual({ id: 'a' });

    act(() => result.current.actions.confirmRemove());
    await settle();
    expect(removeSpy).toHaveBeenCalledWith(apiClient, { id: 'a' });
    expect(result.current.state.remove.target).toBeNull();
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it('keeps the dialog open and surfaces a mapped error on delete failure', async () => {
    removeSpy.mockClear();
    removeSpy.mockResolvedValueOnce({
      error: { code: 9, message: 'still referenced' },
    });
    const { result } = render({});
    act(() => result.current.actions.openRemove({ id: 'a' }));
    act(() => result.current.actions.confirmRemove());
    await settle();
    // FAILED_PRECONDITION (9) surfaces the server message verbatim.
    expect(result.current.state.remove.target).toEqual({ id: 'a' });
    expect(result.current.state.remove.error).toBe('still referenced');
    expect(result.current.state.remove.pending).toBe(false);
  });

  it('closeRemove drops the target without deleting', () => {
    removeSpy.mockClear();
    const { result } = render({});
    act(() => result.current.actions.openRemove({ id: 'a' }));
    act(() => result.current.actions.closeRemove());
    expect(result.current.state.remove.target).toBeNull();
    expect(removeSpy).not.toHaveBeenCalled();
  });
});
