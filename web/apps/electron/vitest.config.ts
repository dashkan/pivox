import { defineConfig } from 'vitest/config';

// Main-process unit tests. The security-critical auth engine (token lifecycle +
// single-flight refresh) is written with NO `electron` imports — its
// electron-coupled dependencies (safeStorage persistence, id-token decode,
// clock) are injected — so it runs under plain Node here. The electron-runtime
// glue (IPC, BrowserWindow, shell) is thin and verified live, not unit-tested.
export default defineConfig({
  test: {
    name: '@pivox/electron',
    dir: './',
    watch: false,
    globals: true,
    environment: 'node',
    include: ['tests/**/*.test.ts'],
  },
});
