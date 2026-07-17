import path from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig, mergeConfig } from 'vitest/config';
import { pivoxViteConfig } from '../../vite.config.shared.js';
import packageJson from './package.json';

const config = defineConfig({
  plugins: [tailwindcss()],
  build: {
    rollupOptions: {
      onwarn(warning, warn) {
        if (warning.code === 'MODULE_LEVEL_DIRECTIVE') return;
        if (warning.code === 'SOURCEMAP_ERROR') return;
        warn(warning);
      },
    },
  },
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  test: {
    name: packageJson.name,
    dir: './',
    watch: false,
    globals: true,
    environment: 'node',
    include: ['tests/**/*.test.{ts,tsx}'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html', 'lcov'],
      exclude: [
        'node_modules/',
        'dist/',
        'tests/',
        '**/*.test.ts',
        '**/*.config.ts',
        '**/types.ts',
      ],
      include: ['src/**/*.ts'],
    },
  },
});

export default mergeConfig(
  config,
  pivoxViteConfig({
    entry: [
      './src/api.ts',
      './src/create-org.ts',
      './src/org-gate.ts',
      './src/auth.ts',
      './src/auth-gate.ts',
      './src/app-shell.ts',
      './src/image-editor.ts',
      './src/chat.ts',
      './src/workflows.ts',
      './src/connectors.ts',
      './src/secrets.ts',
    ],
    srcDir: './src',
  }),
);
