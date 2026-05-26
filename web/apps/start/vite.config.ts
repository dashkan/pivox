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
        //
        // `google-auth-library` is a Node-only package that we
        // consume directly in pivox-actor-token.ts for SSR
        // iamcredentials.signJwt minting. It has the same
        // bundling-hostile shape as firebase-admin (gRPC transports,
        // dynamic ADC discovery, conditional native module loads),
        // so it lives in externals for identical reasons.
        external: [
          /^@sentry\//,
          'firebase-admin',
          /^firebase-admin\//,
          'google-auth-library',
        ],
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
