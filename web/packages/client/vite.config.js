import path from 'node:path';
import { defineConfig, mergeConfig } from 'vitest/config';
import { pivoxViteConfig } from '../../vite.config.shared.js';
import packageJson from './package.json';

const config = defineConfig({
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
    include: ['tests/**/*.test.ts'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html', 'lcov'],
      exclude: [
        'node_modules/',
        'dist/',
        'tests/',
        '**/*.test.ts',
        '**/*.config.ts',
        'src/generated/**',
      ],
      include: ['src/**/*.ts'],
    },
  },
});

export default mergeConfig(
  config,
  pivoxViteConfig({
    entry: [
      './src/index.ts',
      './src/react-query.ts',
      './src/generated/types.gen.ts',
    ],
    srcDir: './src',
  }),
);
