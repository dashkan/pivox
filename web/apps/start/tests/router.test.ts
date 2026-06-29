import { QueryClient } from '@tanstack/react-query';
import { describe, expect, it, vi } from 'vitest';

// Spies on `createRouter` and `setupRouterSsrQueryIntegration` to
// capture the options bags. Defined inside `vi.hoisted` so vitest's
// mock-hoisting can reference them from the `vi.mock` factory below —
// otherwise a bare `vi.fn()` would be undefined when the factory runs.
const { createRouterSpy, ssrSetupSpy } = vi.hoisted(() => ({
  createRouterSpy: vi.fn((opts: unknown) => ({ options: opts })),
  ssrSetupSpy: vi.fn(),
}));

vi.mock('../src/routeTree.gen', () => ({
  routeTree: { stub: true },
}));
vi.mock('@pivox/observability', () => ({
  installErrorReporters: () => undefined,
  installWebTracing: () => undefined,
}));
vi.mock('@tanstack/react-router', () => ({
  createRouter: createRouterSpy,
}));
vi.mock('@tanstack/react-router-ssr-query', () => ({
  // The real integration mutates `router.options.Wrap`; here we
  // just record the call so we can assert it received the SAME
  // QueryClient instance that was passed to createRouter.
  setupRouterSsrQueryIntegration: ssrSetupSpy,
}));

interface RouterOpts {
  context: { queryClient: QueryClient };
}

describe('getRouter', () => {
  it('constructs a fresh QueryClient on every call', async () => {
    createRouterSpy.mockClear();
    const { getRouter } = await import('../src/router');

    getRouter();
    getRouter();

    expect(createRouterSpy).toHaveBeenCalledTimes(2);
    const qcA = (createRouterSpy.mock.calls[0]?.[0] as RouterOpts).context
      .queryClient;
    const qcB = (createRouterSpy.mock.calls[1]?.[0] as RouterOpts).context
      .queryClient;

    expect(qcA).toBeInstanceOf(QueryClient);
    expect(qcB).toBeInstanceOf(QueryClient);
    // Load-bearing assertion. If someone reintroduces a module-level
    // singleton, this fails — and that's exactly the SSR cross-user
    // cache-leak shape we're guarding against.
    expect(qcA).not.toBe(qcB);
  });

  it('passes the QueryClient via router context', async () => {
    createRouterSpy.mockClear();
    const { getRouter } = await import('../src/router');

    getRouter();
    const opts = createRouterSpy.mock.calls[0]?.[0] as RouterOpts;
    expect(opts.context.queryClient).toBeInstanceOf(QueryClient);
  });

  it('hands the same QueryClient instance to the SSR integration', async () => {
    // Guards the dehydrate/hydrate wiring: if someone decouples the
    // QueryClient passed to createRouter from the one passed to
    // setupRouterSsrQueryIntegration, queries prefetched in route
    // loaders never end up in the cache the components read from.
    createRouterSpy.mockClear();
    ssrSetupSpy.mockClear();
    const { getRouter } = await import('../src/router');

    getRouter();
    const routerOpts = createRouterSpy.mock.calls[0]?.[0] as RouterOpts;
    const ssrOpts = ssrSetupSpy.mock.calls[0]?.[0] as {
      router: unknown;
      queryClient: QueryClient;
    };
    expect(ssrOpts.queryClient).toBe(routerOpts.context.queryClient);
  });
});
