import path from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig, mergeConfig } from 'vitest/config';
import { tanstackViteConfig } from '@tanstack/vite-config';
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
  tanstackViteConfig({
    entry: [
      './src/login.ts',
      './src/registration.ts',
      './src/forgot-password.ts',
      './src/reset-password.ts',
      './src/auth.ts',
      './src/verify-email.ts',
      './src/link-account.ts',
      './src/auth-avatar.ts',
      './src/user-profile.ts',
      './src/app-layout.ts',
      './src/image-editor.ts',
    ],
    srcDir: './src',
    cjs: false,
  }),
);
