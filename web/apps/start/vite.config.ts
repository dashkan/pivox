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
    // Pin to IPv4 loopback so the dev server's bind doesn't depend on
    // how the OS resolver orders `localhost` (::1 vs 127.0.0.1). That
    // ordering flipped under macOS 27, moving the server off ::1 and
    // breaking the envoy `web_app` upstream (which targets 127.0.0.1,
    // like every other cluster). Deterministic now, regardless of OS.
    host: '127.0.0.1',
    allowedHosts: ['localhost', 'pivox.ngrok.app'],
  },
  build: {
    rollupOptions: {
      output: {
        // Manual chunk groups for the client bundle. Each long-lived
        // vendor dep gets its own chunk so it caches independently
        // of app code — users only re-download what actually
        // changed on subsequent visits.
        //
        // Uses Rolldown's `codeSplitting` object form. This is the
        // current API; `manualChunks` (function) is ignored when
        // `codeSplitting: true` is set by other plugins (which
        // TanStack Start does), and `advancedChunks` is deprecated.
        //
        // Empirically:
        //   - First matching group wins. Order list specific → broad.
        //   - `priority` field exists but produced inconsistent
        //     results during testing; order alone is reliable.
        //   - The TSR router-plugin only claims modules from your
        //     `routes/` directory (turning route components into
        //     lazy chunks). It does NOT claim @tanstack/* runtime
        //     packages — those split into vendor chunks cleanly.
        //
        // Heuristic: split anything > ~30KB raw and rarely-changing.
        // Smaller deps stay in the main bundle — splitting them
        // costs an HTTP request without meaningful caching benefit.
        codeSplitting: {
          groups: [
            // Order matters: each module routes to the first
            // matching group. Specific names before generic.
            //
            // What's NOT here:
            // - @tanstack/react-start: server-only for our usage
            //   (createServerFn becomes a client-side RPC stub, all
            //   real runtime lives in the SSR pass). Tested with an
            //   explicit group; produced no chunk because nothing in
            //   the client bundle matches.
            // - The TSR router-plugin's route-component splits live
            //   in their own per-route chunks (login, register, etc).
            //   Those are orthogonal to these vendor groups — route
            //   chunks emit ES imports that pull React, Radix, etc.
            //   from the vendor chunks below.
            {
              name: 'react',
              test: /[\\/]node_modules[\\/](react|react-dom|scheduler|use-sync-external-store)[\\/]/,
            },
            // TanStack Router core + history + ssr-query bridge.
            // ~115KB. The library is updated less often than app
            // code; splitting it gets ~115KB of cache wins on every
            // visit after the first deploy.
            {
              name: 'tanstack-router',
              test: /[\\/]node_modules[\\/]@tanstack[\\/](react-router|router-core|history|react-router-ssr-query)[\\/]/,
            },
            // TanStack Query + openapi-react-query binding. ~40KB.
            // Separate chunk so router-version churn doesn't
            // invalidate the query chunk and vice versa.
            {
              name: 'tanstack-query',
              test: /[\\/]node_modules[\\/](@tanstack[\\/](react-query|query-core|query-devtools)|openapi-react-query)[\\/]/,
            },
            // Firebase auth + app SDK. Heaviest vendor at ~190KB,
            // updated rarely. Loaded eagerly because @pivox/features
            // hooks statically import from firebase/auth.
            {
              name: 'firebase',
              test: /[\\/]node_modules[\\/](firebase|@firebase)[\\/]/,
            },
            // Radix UI primitives + their transitive runtime helpers
            // (aria-hidden, react-remove-scroll, etc.). ~65KB across
            // our usage.
            {
              name: 'radix',
              test: /[\\/]node_modules[\\/](@radix-ui|aria-hidden|react-remove-scroll|react-style-singleton|use-callback-ref|use-sidecar|get-nonce)/,
            },
          ],
        },
      },
    },
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
