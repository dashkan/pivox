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
      './src/login-card.ts',
      './src/registration-card.ts',
      './src/create-org-card.ts',
      './src/forgot-password-card.ts',
      './src/reset-password-card.ts',
      './src/verify-email-card.ts',
      './src/link-account-card.ts',
      './src/user-avatar.ts',
      './src/user-profile-card.ts',
      './src/app-shell.ts',
      './src/theme-switcher.ts',
      './src/sidebar-provider.ts',
      './src/image-editor.ts',
      './src/chat.ts',
      './src/workflow.ts',
      './src/resource-admin.ts',
    ],
    srcDir: './src',
  }),
);
