import type { Plugin } from 'vite';
import { defineConfig } from 'vitest/config';

import baseConfig from './vite.config';

// Reuse the app's vite config (aliases, route plugins, resolve conditions) for
// tests, but strip the TanStack Start *server-function transform* plugins.
//
// Those plugins rewrite `createServerFn(...).handler(fn)` into an SSR-RPC stub
// (`createSsrRpc(id)`) that dispatches through a server manifest which doesn't
// exist under vitest — so any unit test that imports a server fn (e.g.
// `getServerSession`) gets an uncallable stub. Removing only the two transform
// plugins leaves the rest of the Start/router pipeline intact while letting
// tests `vi.mock('@tanstack/react-start')`'s `createServerFn` so `.handler(fn)`
// returns the raw handler for direct, deterministic branch testing.
const STRIPPED_PLUGINS = new Set([
  'tanstack-start-core::server-fn:ssr',
  'tanstack-start-core::server-fn:client',
]);

const basePlugins = ((baseConfig.plugins ?? []) as Plugin[])
  .flat(Infinity as 1)
  .filter((p): p is Plugin => Boolean(p) && !STRIPPED_PLUGINS.has(p.name));

export default defineConfig({
  ...baseConfig,
  plugins: basePlugins,
});
