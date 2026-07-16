// @vitest-environment jsdom
import { renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { ReactQueryApi } from '@pivox/client/react-query';
import type { WorkflowGraph } from '@/workflows/transform/ast-to-graph';
import type { WorkflowVersion } from '@/workflows/transform/types';

import { useWorkflowDefinition } from '@/workflows/use-workflow-definition';

import { httpStep, seq, setStep, version } from './ast-fixtures';

// Control layout resolution timing so version-switch races are deterministic.
type Deferred = {
  promise: Promise<WorkflowGraph>;
  resolve: (graph: WorkflowGraph) => void;
};

function defer(): Deferred {
  let resolve!: (graph: WorkflowGraph) => void;
  const promise = new Promise<WorkflowGraph>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

const layoutMock = vi.fn<(graph: WorkflowGraph) => Promise<WorkflowGraph>>();
vi.mock('@/workflows/transform/layout', () => ({
  layoutGraph: (graph: WorkflowGraph) => layoutMock(graph),
}));

afterEach(() => {
  layoutMock.mockReset();
});

type QueryResult = {
  data?: unknown;
  isLoading?: boolean;
  error?: unknown;
};

/**
 * Fake `$api` whose `useQuery` dispatches on the path template. `enabled: false`
 * (a disabled query) surfaces as a resolved-but-empty result, matching
 * react-query's shape.
 */
function fakeApi(byPath: Record<string, QueryResult>): ReactQueryApi {
  const useQuery = (
    _method: string,
    path: string,
    _init: unknown,
    options?: { enabled?: boolean },
  ): QueryResult => {
    if (options && options.enabled === false) {
      return { data: undefined, isLoading: false, error: undefined };
    }
    return byPath[path] ?? { data: undefined, isLoading: true, error: undefined };
  };
  return { useQuery } as unknown as ReactQueryApi;
}

const VERSION_ORG =
  '/v1/organizations/{organization}/workflows/{workflow}/versions/{version}';
const WORKFLOW_ORG = '/v1/organizations/{organization}/workflows/{workflow}';

const orgVersionName = 'organizations/acme/workflows/wf/versions/3';
const orgWorkflowName = 'organizations/acme/workflows/wf';

const v1: WorkflowVersion = { name: orgVersionName, ...version(seq(httpStep('a'))) };

describe('useWorkflowDefinition', () => {
  it('runs the transform and resolves the laid-out graph for a version name', async () => {
    const laid: WorkflowGraph = { nodes: [], edges: [] };
    layoutMock.mockResolvedValue(laid);

    const $api = fakeApi({
      [VERSION_ORG]: { data: v1, isLoading: false, error: undefined },
    });

    const { result } = renderHook(() =>
      useWorkflowDefinition({ $api, name: orgVersionName }),
    );

    // Layout is async: the graph is not ready on the first commit.
    expect(result.current.graph).toBeNull();
    expect(result.current.isLoading).toBe(true);

    await waitFor(() => expect(result.current.graph).toBe(laid));
    expect(result.current.isLoading).toBe(false);
    expect(result.current.error).toBeNull();

    // astToGraph output (not the raw version) is what gets laid out.
    expect(layoutMock).toHaveBeenCalledTimes(1);
    expect(layoutMock.mock.calls[0]?.[0]).toMatchObject({
      nodes: expect.any(Array),
    });
  });

  it('re-runs layout on version switch and ignores the stale resolution', async () => {
    const d1 = defer();
    const d2 = defer();
    layoutMock.mockReturnValueOnce(d1.promise).mockReturnValueOnce(d2.promise);

    const v2: WorkflowVersion = {
      name: 'organizations/acme/workflows/wf/versions/4',
      ...version(seq(setStep('b'))),
    };

    const { result, rerender } = renderHook(
      ({ v }: { v: WorkflowVersion }) =>
        useWorkflowDefinition({
          $api: fakeApi({
            [VERSION_ORG]: { data: v, isLoading: false, error: undefined },
          }),
          name: v.name ?? '',
        }),
      { initialProps: { v: v1 } },
    );

    await waitFor(() => expect(layoutMock).toHaveBeenCalledTimes(1));

    // Switch versions before the first layout resolves.
    rerender({ v: v2 });
    await waitFor(() => expect(layoutMock).toHaveBeenCalledTimes(2));

    // The stale (v1) layout resolves last — it must not surface.
    const staleGraph: WorkflowGraph = { nodes: [{ id: 'stale' } as never], edges: [] };
    const freshGraph: WorkflowGraph = { nodes: [{ id: 'fresh' } as never], edges: [] };
    d2.resolve(freshGraph);
    d1.resolve(staleGraph);

    await waitFor(() => expect(result.current.graph).toBe(freshGraph));
    expect(result.current.graph).not.toBe(staleGraph);
  });

  it('resolves a Workflow name through GetWorkflow then GetWorkflowVersion', async () => {
    const laid: WorkflowGraph = { nodes: [], edges: [] };
    layoutMock.mockResolvedValue(laid);

    const $api = fakeApi({
      [WORKFLOW_ORG]: {
        data: { name: orgWorkflowName, version: orgVersionName },
        isLoading: false,
        error: undefined,
      },
      [VERSION_ORG]: { data: v1, isLoading: false, error: undefined },
    });

    const { result } = renderHook(() =>
      useWorkflowDefinition({ $api, name: orgWorkflowName }),
    );

    await waitFor(() => expect(result.current.graph).toBe(laid));
    expect(result.current.error).toBeNull();
  });

  it('reports loading while the version query is in flight', () => {
    const $api = fakeApi({
      [VERSION_ORG]: { data: undefined, isLoading: true, error: undefined },
    });
    const { result } = renderHook(() =>
      useWorkflowDefinition({ $api, name: orgVersionName }),
    );
    expect(result.current.isLoading).toBe(true);
    expect(result.current.graph).toBeNull();
    expect(layoutMock).not.toHaveBeenCalled();
  });

  it('surfaces a query error as a domain message', () => {
    const $api = fakeApi({
      [VERSION_ORG]: {
        data: undefined,
        isLoading: false,
        error: { code: 5, message: 'version not found' },
      },
    });
    const { result } = renderHook(() =>
      useWorkflowDefinition({ $api, name: orgVersionName }),
    );
    expect(result.current.error).toBe('version not found');
    expect(result.current.graph).toBeNull();
  });

  it('throws loudly for a space-scoped name (unwired path)', () => {
    const $api = fakeApi({});
    const spaceVersion =
      'organizations/acme/spaces/main/workflows/wf/versions/3';
    // Suppress React's error-boundary console noise for the expected throw.
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() =>
      renderHook(() => useWorkflowDefinition({ $api, name: spaceVersion })),
    ).toThrow(/space-scoped names are not supported/);
    spy.mockRestore();
  });

  it('stays idle for a workflow with no promoted version', () => {
    const $api = fakeApi({
      [WORKFLOW_ORG]: {
        data: { name: orgWorkflowName, version: '' },
        isLoading: false,
        error: undefined,
      },
    });
    const { result } = renderHook(() =>
      useWorkflowDefinition({ $api, name: orgWorkflowName }),
    );
    expect(result.current.graph).toBeNull();
    expect(result.current.isLoading).toBe(false);
    expect(result.current.error).toBeNull();
    expect(layoutMock).not.toHaveBeenCalled();
  });
});
