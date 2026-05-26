import { globalIgnores } from 'eslint/config';

import { reactVite } from '../../eslint.config.js';

export default [
  ...reactVite,
  {
    // TanStack Router file-based routes require `export const Route = createFileRoute(...)`
    // co-located with the route component. The Fast Refresh "only export components" rule
    // can't accommodate that even with allowExportNames — disable for routes.
    files: ['**/routes/**/*.{ts,tsx}'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
  // public/ holds static assets served as-is (e.g. theme-init.js,
  // the FOUC-prevention bootstrap loaded synchronously before React).
  // Not part of the TypeScript project — eslint's project-service
  // can't type-check it and errors out otherwise.
  globalIgnores([
    '**/node_modules',
    '**/dist',
    '**/out',
    'src/renderer/public/**',
  ]),
];
