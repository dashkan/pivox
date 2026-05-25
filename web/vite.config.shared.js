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
            process.exit(1);
          }
        },
      }),
    ],
    build: {
      outDir,
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
