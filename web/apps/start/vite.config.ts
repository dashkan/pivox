import { defineConfig } from 'vite';
import { devtools } from '@tanstack/devtools-vite';

import { tanstackStart } from '@tanstack/react-start/plugin/vite';

import viteReact from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { nitro } from 'nitro/vite';

const config = defineConfig({
  server: {
    allowedHosts: ['localhost', 'pivox.ngrok.app'],
  },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [
    devtools(),
    nitro({
      rollupConfig: {
        // `firebase-admin` is a Node-only package with CJS-style
        // circular module references and dynamic init code that
        // doesn't survive Rollup bundling — bundling it crashes
        // at SSR time with "Cannot read properties of undefined
        // (reading 'SDK_VERSION')" because the property read happens
        // before the bundled init order completes the export.
        // Marking it external keeps it as a runtime `import` resolved
        // from node_modules, which is the only shape that works.
        // Same pattern applies to the auth subpath we actually
        // consume from server-side code (auth-session.ts).
        external: [/^@sentry\//, 'firebase-admin', /^firebase-admin\//],
      },
      routeRules: {
        '/__/auth/**': {
          proxy: 'https://pivox-cloud.firebaseapp.com/__/auth/**',
        },
      },
    }),
    tailwindcss(),
    tanstackStart(),
    viteReact(),
  ],
});

export default config;
