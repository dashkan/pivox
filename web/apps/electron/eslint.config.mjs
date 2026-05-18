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
  globalIgnores(['**/node_modules', '**/dist', '**/out']),
];
