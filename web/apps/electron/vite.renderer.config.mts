import { resolve } from 'node:path';

import { buildBootScript } from '@pivox/storage';
import tailwindcss from '@tailwindcss/vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';
import type { Plugin } from 'vite';

/**
 * Serves/emits `bootstrap.js` — @pivox/storage's pre-paint script (theme, etc.),
 * the same source of truth the start app inlines during SSR.
 *
 * External rather than inline because the renderer's CSP is `script-src 'self'`;
 * inlining would need `'unsafe-inline'` or per-build hash maintenance.
 *
 * The tag is injected (not written into index.html) so it can use a RELATIVE
 * src: the packaged renderer loads over file://, where a root-absolute
 * `/bootstrap.js` resolves against the filesystem root and 404s — silently
 * killing the pre-paint bootstrap in packaged builds only. It stays pre-paint
 * because it's a classic script (parse-time) while Vite's entry is a module
 * (deferred).
 */
function pivoxStorageBootScript(): Plugin {
  // Computed per-request/per-build, not at config load: the item registry is
  // populated by import side effects, so a late-registered StorageItem reachable
  // only from the app's module graph would otherwise be missed.
  return {
    name: 'pivox-storage-boot-script',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
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
    generateBundle() {
      this.emitFile({
        type: 'asset',
        fileName: 'bootstrap.js',
        source: buildBootScript(),
      });
    },
    transformIndexHtml() {
      // 'head' (append), not 'head-prepend': must land after the CSP <meta>.
      return [
        {
          tag: 'script',
          attrs: { src: './bootstrap.js' },
          injectTo: 'head' as const,
        },
      ];
    },
  };
}

// https://vitejs.dev/config
export default defineConfig({
  resolve: {
    // plugin-vite's renderer default is `preserveSymlinks: true`, which breaks
    // pnpm: deps resolve to their symlink, so a transitive like
    // @tanstack/query-core is looked up beside the symlink instead of in the
    // .pnpm store, and the build fails to resolve it.
    preserveSymlinks: false,
    alias: {
      '@renderer': resolve(__dirname, 'src/renderer'),
    },
    dedupe: ['react', 'react-dom'],
  },
  plugins: [
    tailwindcss(),
    tanstackRouter({
      target: 'react',
      autoCodeSplitting: true,
      routesDirectory: resolve(__dirname, 'src/renderer/routes'),
      generatedRouteTree: resolve(__dirname, 'src/renderer/routeTree.gen.ts'),
    }),
    react(),
    pivoxStorageBootScript(),
  ],
});
