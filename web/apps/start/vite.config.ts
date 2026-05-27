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
    // Force `tslib` to its proper ESM entry. The package's CJS
    // build (`tslib.js`) sets `module.exports.__esModule = true`
    // via createExporter, which confuses Vite/Rollup's `__toESM`
    // helper into skipping the synthetic `.default` — then the
    // generated `var { __extends, ... } = __toESM(...).default;`
    // destructure throws at SSR load (`.default` is undefined).
    //
    // tslib ships an ESM build at `modules/index.js` (advertised
    // via the package's `exports.import.node` condition) that does
    // a clean re-export with named bindings. Aliasing the bare
    // specifier directly to that path bypasses Vite's resolution
    // (which was picking the CJS entry) and lets the bundler emit
    // a normal ESM import — no `__toESM`, no broken default
    // destructure. Affects both client and SSR builds.
    // Alias `tslib` to its native ESM build (`tslib.es6.mjs`). The
    // default `tslib.js` is CJS with a factory pattern that sets
    // `__esModule = true` — Vite/Rollup's `__toESM` helper then
    // skips the synthetic `.default` while the generated bundle
    // code destructures from `.default` anyway, throwing
    // `Cannot destructure property '__extends' of '__toESM(...).default'`
    // on SSR load. Transitive Radix UI deps (aria-hidden,
    // react-remove-scroll) all do `import { __assign } from 'tslib'`
    // and trip the bug. `tslib.es6.mjs` is real ESM with native
    // named exports — no interop, no destructure bug.
    //
    // Pointing at the inner `modules/index.js` doesn't help because
    // that file is itself an ESM facade that imports the CJS
    // `tslib.js` as default and re-exports — same interop problem
    // one indirection deeper.
    alias: [
      {
        find: /^tslib$/,
        replacement:
          new URL(
            '../../node_modules/.pnpm/tslib@2.8.1/node_modules/tslib/tslib.es6.mjs',
            import.meta.url,
          ).pathname,
      },
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
