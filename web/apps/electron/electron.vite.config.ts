import { resolve } from 'node:path';
import { buildBootScript } from '@pivox/storage';
import { defineConfig } from 'electron-vite';
import type { Plugin } from 'vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

/**
 * Generates `/bootstrap.js` from @pivox/storage's `buildBootScript()`
 * — same source of truth the start app uses for its SSR inline script.
 *
 * Two reasons we serve it as an EXTERNAL script (not inline like
 * start does):
 *   1. Electron's CSP has `script-src 'self'` and accepting inline
 *      scripts would either require `'unsafe-inline'` (too broad) or
 *      per-build hash maintenance. External same-origin scripts are
 *      covered by `'self'` without ceremony.
 *   2. `index.html` is a static asset; React-side template rendering
 *      isn't available the way it is in the SSR pass.
 *
 * buildBootScript's output adapts to the platform at runtime: it
 * branches on `location.protocol` and reads the cookie on http(s)
 * origins (the start app) or localStorage on file:// (electron).
 * Same item registry + per-item try/catch on both sides. Adding a
 * new pre-mount setting (theme today, font-size or motion-reduce
 * tomorrow) just means defining a new StorageItem with an `onBoot`
 * in @pivox/storage's items.ts — both apps pick it up automatically.
 */
function pivoxStorageBootScript(): Plugin {
  // buildBootScript() is computed at REQUEST time (dev) and at
  // EMIT time (build) rather than at config-load — the registry is
  // populated by side effects on `@pivox/storage/items.ts` import,
  // and if any future StorageItem is registered by a module not
  // reachable from this config file (e.g., a feature module imported
  // only from main.tsx), a config-load computation would ship a
  // partial registry. Re-computing per request / per build catches
  // late-registered items via the bundle's module graph. Cost is
  // negligible — the items.ts side effect runs once per process and
  // toString() is fast.
  return {
    name: 'pivox-storage-boot-script',
    // Dev: respond to `/bootstrap.js` with the generated script.
    // Vite's public/ static middleware doesn't run for this path
    // because we don't ship a static fallback — the plugin owns it.
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        // Strip query string + hash before matching so cache-busted
        // variants (`/bootstrap.js?v=abc123`, sourcemap query, etc.)
        // still hit this middleware. URL is request-relative, so a
        // dummy base lets the URL parser accept it.
        const pathname = req.url ? new URL(req.url, 'http://x').pathname : null;
        if (pathname === '/bootstrap.js') {
          res.setHeader(
            'Content-Type',
            'application/javascript; charset=utf-8',
          );
          res.end(buildBootScript());
          return;
        }
        next();
      });
    },
    // Build: emit the same content as a real asset. `bootstrap.js`
    // lands at the renderer dist root and index.html's
    // `<script src="/bootstrap.js">` resolves normally.
    generateBundle() {
      this.emitFile({
        type: 'asset',
        fileName: 'bootstrap.js',
        source: buildBootScript(),
      });
    },
  };
}

export default defineConfig({
  main: {
    build: {
      // The main process runs the OIDC flow via `@pivox/oidc`, which (and whose
      // OIDC deps: openid-client → jose, oauth4webapi) is ESM-only with no CJS
      // `require` export. electron-vite externalizes deps for the CJS main by
      // default, which would leave a runtime `require('@pivox/oidc')` that fails
      // to resolve. Exclude the whole ESM-only tree so Vite bundles it into main.
      externalizeDeps: {
        exclude: ['@pivox/oidc', 'openid-client', 'jose', 'oauth4webapi'],
      },
    },
  },
  preload: {},
  renderer: {
    resolve: {
      alias: {
        '@renderer': resolve('src/renderer/src'),
      },
      dedupe: ['react', 'react-dom'],
    },
    plugins: [
      tailwindcss(),
      tanstackRouter({
        target: 'react',
        autoCodeSplitting: true,
        routesDirectory: resolve('src/renderer/src/routes'),
        generatedRouteTree: resolve('src/renderer/src/routeTree.gen.ts'),
      }),
      react(),
      pivoxStorageBootScript(),
    ],
  },
});
