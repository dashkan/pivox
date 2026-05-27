import {
  defaultClientConditions,
  defaultServerConditions,
  defineConfig,
} from 'vite';
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
    // Restore Vite's default resolve conditions explicitly. Some
    // framework plugin upstream of us (Nitro, TanStack Start, or one
    // of their transitive integrations) sets `conditions: []` during
    // SSR config-merge, which strips the `module`/`import`/`node`
    // conditions that make `tslib`'s `exports` field resolve to its
    // ESM entry. Without those conditions, tslib falls through to
    // its CJS `require` entry — which has the `__esModule: true`
    // flag that trips Rolldown's `__toESM` interop (returns the
    // module unwrapped, but generated bundle still destructures
    // `.default`, throwing `Cannot destructure property '__extends'
    // of __toESM(...).default` on SSR load).
    //
    // Pulled in transitively by Radix UI deps (aria-hidden,
    // react-remove-scroll) that compile with `importHelpers: true`.
    //
    // This is the Vite maintainer's recommended fix per the
    // discussion at vitejs/vite#19032 (cross-referenced from
    // remix-run/react-router#12610). Restoring the defaults lets
    // tslib's `exports.import` win for non-`require` consumers, so
    // Rolldown receives the ESM build directly — no broken interop.
    //
    // See: https://vite.dev/guide/migration.html#default-value-for-resolve-conditions
    conditions: [...defaultClientConditions],
  },
  ssr: {
    // Same conditions-restoration as `resolve.conditions` above —
    // applies to SSR-side module resolution, which is the path that
    // tripped the tslib bug. Without this, the SSR resolver falls
    // back to tslib's CJS, triggering Rolldown's `__toESM` interop
    // bug on bundle.
    resolve: {
      conditions: [...defaultServerConditions],
    },
    // Externalize `react` and `react-dom` from the SSR build.
    //
    // Without this, the SSR output contains TWO React instances:
    //   1. The bundled React, inlined into _libs/react+tanstack__*
    //      via __commonJSMin (Rollup's CJS shim).
    //   2. A runtime `__require("react")` from inside
    //      `use-sync-external-store/shim`, which the bundler did
    //      NOT transform (the shim's CJS `require('react')` got
    //      left as a Node-side resolution).
    //
    // The TanStack Start SSR renderer sets
    // `ReactSharedInternals.H = HooksDispatcher` on instance #1.
    // Radix UI's Avatar (via `useImageLoadingStatus` →
    // `useIsHydrated` → the shim's `useSyncExternalStore`) reads
    // `ReactSharedInternals.H` from instance #2, finds null, and
    // throws "Cannot read properties of null (reading
    // 'useSyncExternalStore')" every time an Avatar renders
    // server-side.
    //
    // Externalizing both forces every consumer (bundle + shim +
    // Radix + everything else) to share the single Node-installed
    // copy at `node_modules/react`. Both packages are direct deps
    // of apps/start so Nitro includes them in the deployed
    // node_modules.
    external: [
      'react',
      'react-dom',
      'react-dom/server',
      'react-dom/server.edge',
      'react-dom/client',
      'react/jsx-runtime',
      'react/jsx-dev-runtime',
    ],
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
          // `react` and `react-dom` MUST be external in the Nitro
          // _libs/ build too — see the `ssr.external` block above.
          // The Nitro pass builds the `_libs/react+tanstack__*` chunk
          // and inlines React via __commonJSMin by default; that
          // inlined copy doesn't share `ReactSharedInternals` with
          // the `__require("react")` instance loaded by
          // use-sync-external-store/shim, so Radix Avatar's
          // useSyncExternalStore reads from a null dispatcher and
          // throws. Externalizing here unifies on the Node-loaded
          // single instance.
          /^react$/,
          /^react-dom(\/|$)/,
          /^react\/(jsx-runtime|jsx-dev-runtime)$/,
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
