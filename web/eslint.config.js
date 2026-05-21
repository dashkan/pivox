// @ts-check

/**
 * Workspace eslint config — composed from the upstream plugin presets
 * directly. No meta-preset (we used to extend @tanstack/eslint-config;
 * dropped it because it shipped no React-specific rules and pulled in
 * peer-dep mismatches against our pinned eslint runtime).
 *
 * Three variants exported:
 *   - base       : JS/TS + import order + unused-imports (no React)
 *   - reactConfig: base + react + react-hooks + jsx-a11y
 *   - reactVite  : reactConfig + react-refresh (for Vite HMR consumers)
 *
 * Default export is `reactConfig` — library packages (primitives, ui,
 * features, image-editor) fall through to this when eslint walks up
 * the directory tree from their `eslint ./src` invocation. Apps with
 * Vite HMR (start, electron) import the `reactVite` variant from
 * their own per-app config.
 *
 * Pinned to eslint v9 because eslint-plugin-react@7.37.5 (latest)
 * declares peer `eslint: '... || ^9.7'` and has no v10 support yet.
 * See https://github.com/jsx-eslint/eslint-plugin-react/issues/3977.
 */

import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import reactPlugin from 'eslint-plugin-react';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import jsxA11y from 'eslint-plugin-jsx-a11y';
import importX from 'eslint-plugin-import-x';
import unusedImports from 'eslint-plugin-unused-imports';
import globals from 'globals';

const ignores = {
  ignores: [
    '**/dist/**',
    '**/build/**',
    '**/out/**',
    '**/.output/**',
    '**/node_modules/**',
    '**/coverage/**',
    // vendored third-party code — not ours to lint
    '**/shadcn/**',
    '**/vercel/**',
    // generated
    '**/routeTree.gen.ts',
    // build configs themselves
    '**/*.config.{js,ts,mjs,cjs}',
  ],
};

/** @type {import('eslint').Linter.Config[]} */
export const base = [
  ignores,
  js.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
      },
      sourceType: 'module',
      ecmaVersion: 2024,
      globals: { ...globals.browser, ...globals.node },
    },
    plugins: {
      'import-x': importX,
      'unused-imports': unusedImports,
    },
    rules: {
      // Carried over from previous config
      // 'no-case-declarations': 'off',
      // 'no-shadow': 'off',
      // unused-imports/no-unused-imports auto-fixes dead imports;
      // @typescript-eslint/no-unused-vars flags dead locals.
      // 'unused-imports/no-unused-imports': 'warn',
      // 'no-unused-vars': 'off',
      '@typescript-eslint/restrict-template-expressions': [
        'error',
        {
          allowNumber: true,
          allowBoolean: true,
        },
      ],
      '@typescript-eslint/no-unused-vars': [
        'warn',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
        },
      ],
      'import-x/order': [
        'warn',
        {
          'newlines-between': 'always',
          groups: [
            'builtin',
            'external',
            'internal',
            'parent',
            'sibling',
            'index',
            'type',
          ],
          alphabetize: { order: 'asc', caseInsensitive: true },
        },
      ],
    },
  },
];

/** @type {import('eslint').Linter.Config[]} */
export const reactConfig = [
  ...base,
  reactPlugin.configs.flat.recommended,
  reactPlugin.configs.flat['jsx-runtime'],
  jsxA11y.flatConfigs.recommended,
  {
    settings: { react: { version: 'detect' } },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // Modern JSX transform — no need to import React in scope.
      // jsx-runtime config already disables react-in-jsx-scope; be explicit.
      'react/react-in-jsx-scope': 'off',
      // We use TS types, not propTypes.
      'react/prop-types': 'off',
    },
  },
];

/** @type {import('eslint').Linter.Config[]} */
export const reactVite = [
  ...reactConfig,
  {
    plugins: { 'react-refresh': reactRefresh },
    rules: {
      'react-refresh/only-export-components': [
        'warn',
        { allowConstantExport: true },
      ],
    },
  },
];

export default reactConfig;
