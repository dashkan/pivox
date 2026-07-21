import { resolve } from 'node:path';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// Renderer-process tests run in jsdom (the main-process suite in
// `vitest.config.ts` is node-only). Kept a separate config so the two never
// share an environment: main-process code must stay `electron`-import-free and
// run under plain Node, while renderer components need a DOM. Only the
// `@renderer` alias + react-dedupe from `vite.renderer.config.mts` are mirrored
// here — the tanstack-router / tailwind / storage-boot plugins aren't needed to
// render a component in isolation.
export default defineConfig({
  resolve: {
    preserveSymlinks: false,
    alias: {
      '@renderer': resolve(__dirname, 'src/renderer'),
    },
    dedupe: ['react', 'react-dom'],
  },
  plugins: [react()],
  test: {
    name: '@pivox/electron-renderer',
    watch: false,
    globals: true,
    environment: 'jsdom',
    include: ['tests/renderer/**/*.test.tsx'],
  },
});
