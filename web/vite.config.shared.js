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
 * What's left mirrors tanstack: dts() for declaration generation,
 * externalizeDeps() for runtime-dep externalization, build.lib for
 * library mode with preserveModules.
 */

import { defineConfig } from 'vite';
import dts from 'vite-plugin-dts';
import { externalizeDeps } from 'vite-plugin-externalize-deps';

/**
 * Rewrites relative import specifiers in .d.ts output to include the
 * `.js` extension. Required for downstream consumers under
 * "moduleResolution": "Bundler" / "NodeNext" — TS resolves declaration
 * imports against the on-disk filename, not the bare specifier.
 *
 * @param {{ content: string }} args
 */
function ensureImportFileExtension({ content }) {
  content = content.replace(
    /(im|ex)port\s[\w{}/*\s,]+from\s['"](?:\.\.?\/)+?[^.'"]+(?=['"];?)/gm,
    '$&.js',
  );
  content = content.replace(
    /import\(['"](?:\.\.?\/)+?[^.'"]+(?=['"];?)/gm,
    '$&.js',
  );
  return content;
}

/**
 * @param {{
 *   entry: string | string[],
 *   srcDir: string,
 *   outDir?: string,
 *   externalDeps?: (string | RegExp)[],
 *   bundledDeps?: (string | RegExp)[],
 *   exclude?: string[],
 *   beforeWriteDeclarationFile?: (filePath: string, content: string) => string | undefined,
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
      dts({
        // vite-plugin-dts v5 renamed `outDir` → `outDirs` (plural,
        // single or array). Old `outDir` is silently ignored and the
        // plugin defaults to emitting at `dist/` instead of
        // `dist/esm/`, which breaks every consumer that resolves
        // types via the `exports.types` paths in package.json.
        outDirs: `${outDir}/esm`,
        entryRoot: options.srcDir,
        include: options.srcDir,
        exclude: options.exclude,
        compilerOptions: {
          // ts.ModuleKind.ESNext === 99
          module: 99,
          declarationMap: false,
        },
        beforeWriteFile: (filePath, content) => {
          content =
            options.beforeWriteDeclarationFile?.(filePath, content) || content;
          return {
            filePath,
            content: ensureImportFileExtension({ content }),
          };
        },
        afterDiagnostic: (diagnostics) => {
          if (diagnostics.length > 0) {
            console.error('Please fix the above type errors');
            // In the watch dev loop a type error — including a
            // half-saved edit — must NOT kill the watcher: the process
            // would stop rebuilding for the rest of the session and the
            // package would silently go stale. Log and keep watching;
            // the IDE + `test:types` + CI gate real errors. One-shot /
            // CI / publish builds still hard-fail so bad types can't ship.
            if (!isWatch) process.exit(1);
          }
        },
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
