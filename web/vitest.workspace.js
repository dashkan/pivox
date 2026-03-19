// @ts-check

import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    projects: [
      './packages/primitives/vite.config.js',
      './packages/ui/vite.config.js',
      './packages/features/vite.config.js',
    ],
  },
});
