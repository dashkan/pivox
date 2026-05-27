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
  ssr: {
    // SSR-build externals (separate from Nitro's runtime externals).
    // The `nitro({ rollupConfig: { external } })` block below
    // governs Nitro's server bundle; Vite's SSR chunks (the
    // `_ssr/*.mjs` files Rollup emits for SSR rendering) are
    // controlled HERE. Both lists need the same package when a
    // module is hostile to bundling in BOTH passes.
    //
    // `tslib` lives here because of a CJS-vs-ESM-interop bug: tslib
    // sets `__esModule: true` (making Vite's `__toESM` helper skip
    // the synthetic `.default`), but the generated bundle code
    // destructures from `.default` anyway. Any chunk that pulls
    // tslib in (via Radix UI → react-remove-scroll → tslib, etc.)
    // throws `Cannot destructure property '__extends' of
    // '__toESM(...).default' as it is undefined` at SSR-module
    // load. Externalizing leaves the `import 'tslib'` as a runtime
    // require, which Node resolves via the standard ESM/CJS bridge.
    external: ['tslib'],
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
          // `tslib` has a CJS-only shape (factory pattern with
          // `createExporter`) that doesn't survive Rollup's CJS→ESM
          // interop — the synthetic default exposes `__extends`,
          // `__assign`, etc. as `undefined`, and a destructure at the
          // top of any bundled module that imports tslib throws
          // immediately on SSR. Externalizing keeps it as a runtime
          // `import 'tslib'` resolved from node_modules. Radix UI
          // pulls it in transitively (aria-hidden,
          // react-remove-scroll), so anything that touches a Radix
          // component on the SSR pass triggers it.
          'tslib',
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
