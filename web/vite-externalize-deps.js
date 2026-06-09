// @ts-check

import { existsSync, readFileSync } from 'node:fs';
import { builtinModules } from 'node:module';
import { join } from 'node:path';

/**
 * Externalize a package's runtime dependencies from its library bundle.
 *
 * Vendored from `vite-plugin-externalize-deps@0.10.0` — a single-file
 * plugin whose upstream is effectively unmaintained, not worth an npm
 * dependency. Behavior is preserved 1:1.
 *
 * It reads the building package's own `package.json` and marks its
 * `dependencies`, `peerDependencies`, `optionalDependencies`, and Node
 * builtins as Rollup externals, so a `build.lib` build emits bare
 * `import` specifiers for them instead of inlining them into the bundle.
 * A dep `foo` externalizes both `foo` and any subpath `foo/bar`.
 *
 * `build.rollupOptions.external` is honored by Vite 8's Rolldown
 * pipeline (the package builds already rely on it), so no Rolldown-
 * specific handling is needed.
 *
 * @param {{
 *   include?: Array<string | RegExp>,
 *   except?: Array<string | RegExp>,
 *   useFile?: string,
 * }} [options]
 *   - `include`: extra ids to force-externalize (beyond package.json).
 *   - `except`: ids to force-bundle, overriding the externalize rules.
 *   - `useFile`: package.json to read (default: `<cwd>/package.json`).
 * @returns {import('vite').Plugin}
 */
export function externalizeDeps(options = {}) {
  const {
    include = [],
    except = [],
    useFile = join(process.cwd(), 'package.json'),
  } = options;

  return {
    name: 'vite-externalize-deps',
    config() {
      if (!existsSync(useFile)) {
        throw new Error(
          `[vite-externalize-deps] package.json not found at ${useFile}`,
        );
      }

      /** @type {Record<string, unknown>} */
      const pkg = JSON.parse(readFileSync(useFile, 'utf8'));
      const depNames = [
        ...Object.keys(pkg.dependencies ?? {}),
        ...Object.keys(pkg.peerDependencies ?? {}),
        ...Object.keys(pkg.optionalDependencies ?? {}),
      ];

      // Match a dep and any of its subpaths. Names are used as literal
      // regex source (as upstream did) — npm names don't carry regex
      // metacharacters in practice.
      const depPatterns = depNames.map(
        (name) => new RegExp(`^${name}(?:/.+)?$`),
      );
      // Node builtins, with or without the `node:` prefix.
      const builtinPatterns = builtinModules.map(
        (name) => new RegExp(`^(?:node:)?${name}$`),
      );

      /**
       * @param {string} id
       * @param {Array<string | RegExp>} matchers
       */
      const matchesAny = (id, matchers) =>
        matchers.some((m) => (typeof m === 'string' ? m === id : m.test(id)));

      return {
        build: {
          rollupOptions: {
            /** @param {string} id */
            external: (id) => {
              if (matchesAny(id, except)) return false; // force-bundle
              if (matchesAny(id, include)) return true; // force-external
              return (
                depPatterns.some((p) => p.test(id)) ||
                builtinPatterns.some((p) => p.test(id))
              );
            },
          },
        },
      };
    },
  };
}
