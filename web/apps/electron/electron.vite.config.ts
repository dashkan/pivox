import { resolve } from 'node:path';
import { defineConfig } from 'electron-vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  main: {
    build: {
      // `@pivox/features` is an ESM-only workspace package with no CJS
      // (`require`) export condition. electron-vite externalizes deps
      // for the CJS main process by default, which leaves a runtime
      // `require('@pivox/features/broker')` that fails to resolve.
      // Exclude it so Vite bundles it into the main process instead —
      // the intended treatment for first-party workspace packages.
      externalizeDeps: {
        exclude: ['@pivox/features'],
      },
    },
  },
  preload: {},
  renderer: {
    resolve: {
      alias: {
        '@renderer': resolve('src/renderer/src'),
      },
      dedupe: [
        'react',
        'react-dom',
        'firebase',
        'firebase/app',
        'firebase/auth',
      ],
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
    ],
  },
});
