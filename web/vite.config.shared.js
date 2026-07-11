// @ts-check

/**
 * Shared Vite library config — slimmed from @tanstack/vite-config@0.5.2.
 *
 * Inlined here to:
 *   - Drop the `vite-tsconfig-paths` plugin import. Tanstack pulled it in
 *     but only ever registered it when callers passed an explicit
 *     `tsconfigPath`; none of our packages do. Vite 8's native
 *     `resolve.tsconfigPaths: true` (set by callers) handles the default
 *     case without a plugin. Removing this kills the transitive `tsconfck`
 *     dep (which is being EOL'd Q1 2027).
 *   - Drop CJS output entirely. All our consumers are ESM-only and pass
 *     `cjs: false` to tanstack; removing the branch slims this file ~40
 *     lines and removes a confusion vector.
 *
 * What's left mirrors tanstack: declaration generation (now via the
 * native TS7 compiler — see vite-tsgo-dts.js), externalizeDeps() for
 * runtime-dep externalization, build.lib for library mode with
 * preserveModules.
 */

import { defineConfig } from 'vite';
import { externalizeDeps } from './vite-externalize-deps.js';
import { tsgoDts } from './vite-tsgo-dts.js';

/**
 * @param {{
 *   entry: string | string[],
 *   srcDir: string,
 *   outDir?: string,
 *   tsconfig?: string,
 *   externalDeps?: (string | RegExp)[],
 *   bundledDeps?: (string | RegExp)[],
 *   exclude?: string[],
 * }} options
 */
export function pivoxViteConfig(options) {
  const outDir = options.outDir ?? 'dist';

  // Self-detect `vite build --watch` from the CLI args of the process
  // that loads this config — no env var / script coordination needed.
  // (Verified: argv carries `--watch` for watch builds, not one-shot.)
  // Watch is the dev loop; one-shot is `make web-build` / CI / publish.
  const isWatch =
    process.argv.includes('--watch') || process.argv.includes('-w');

  return defineConfig({
    plugins: [
      externalizeDeps({
        include: options.externalDeps ?? [],
        except: options.bundledDeps ?? [],
      }),
      // Declarations emit into `${outDir}/esm` (mirroring the
      // preserveModules JS layout) so consumers resolve types via the
      // `exports.types` paths in package.json. tsgo does the emit; the
      // plugin then rewrites `@/*` → relative + `.js`. Type checking is
      // the `test:types` gate, so we emit with `--noCheck` for speed —
      // in watch this also means a half-saved edit never kills the loop.
      tsgoDts({
        tsconfig: options.tsconfig ?? './tsconfig.json',
        srcDir: options.srcDir,
        outDir,
        noCheck: true,
      }),
    ],
    build: {
      outDir,
      // The watch dev loop must not wipe dist between rebuilds. Parallel
      // package watchers type-check against each other's emitted .d.ts;
      // a wipe window makes a dependency's declarations momentarily
      // missing → spurious diagnostics in the dependent. Overwriting in
      // place keeps every dependency's types present on disk. One-shot
      // builds DO empty, for clean orphan-free output (prod / publish).
      emptyOutDir: !isWatch,
      minify: false,
      sourcemap: true,
      lib: {
        entry: options.entry,
        formats: ['es'],
        fileName: () => 'esm/[name].js',
      },
      rolldownOptions: { output: { preserveModules: true } },
    },
  });
}
