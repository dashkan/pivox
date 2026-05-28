import { defineConfig, mergeConfig } from 'vitest/config';
import { pivoxViteConfig } from '../../vite.config.shared.js';
import packageJson from './package.json';

const config = defineConfig({
  build: {
    rollupOptions: {
      onwarn(warning, warn) {
        if (warning.code === 'MODULE_LEVEL_DIRECTIVE') return;
        if (warning.code === 'SOURCEMAP_ERROR') return;
        warn(warning);
      },
    },
  },
  test: {
    name: packageJson.name,
    dir: './',
    watch: false,
    globals: true,
    // jsdom for tests that exercise document.cookie + localStorage.
    // The package's runtime IS browser-side, so node-env tests would
    // need a polyfill stack heavier than the SUT itself.
    environment: 'jsdom',
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
        '**/types.ts',
      ],
      include: ['src/**/*.ts'],
    },
  },
});

export default mergeConfig(
  config,
  pivoxViteConfig({
    // Three entries:
    //   - root API (`@pivox/storage`)
    //   - React hooks (`@pivox/storage/react`) — kept off the root
    //     so non-React consumers (SSR `prefs.ts`) don't pull React
    //     transitively
    //   - test-utils (`@pivox/storage/test-utils`) — keeps
    //     `__resetRegistryForTests` / `__resetChannelForTests`
    //     out of the production bundle surface
    // Matches the `exports` map in package.json.
    entry: ['./src/index.ts', './src/react.ts', './src/test-utils.ts'],
    srcDir: './src',
  }),
);
