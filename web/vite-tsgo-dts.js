// @ts-check
/**
 * Declaration generation via the native TypeScript 7 compiler (tsgo),
 * replacing unplugin-dts. tsgo emits `.d.ts` ~10x faster than the
 * legacy JS-API compiler unplugin-dts drives (measured on @pivox/primitives:
 * 3647ms → 361ms). tsgo cannot rewrite path aliases on emit (TS has never
 * done this — microsoft/TypeScript#10866), so it emits our `@/` specifiers
 * literally; `tsc-alias` post-processes the emitted `.d.ts` to rewrite
 * `@/*` → relative and append the `.js` extension consumers need under
 * `moduleResolution: Bundler`/`NodeNext`.
 *
 * Type CHECKING is intentionally decoupled (`--noCheck`): each package's
 * `test:types` script (`tsc`) is the type gate. This step only EMITS.
 */

import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { replaceTscAliasPaths } from 'tsc-alias';

const require = createRequire(import.meta.url);

/**
 * Resolve the native tsgo binary shipped inside the `typescript` v7
 * package (platform exe located via its `lib/getExePath.js`). Falls back
 * to `@typescript/native-preview` for pre-GA setups — mirrors how
 * rolldown-plugin-dts resolves it.
 * @returns {Promise<string>}
 */
async function resolveTsgoPath() {
  const tsPkg = require.resolve('typescript/package.json');
  const { default: getExePath } = await import(
    new URL('lib/getExePath.js', `file://${tsPkg}`).href
  );
  return getExePath();
}

/**
 * @param {{ tsconfig: string, srcDir: string, outDir: string, noCheck?: boolean }} options
 * @returns {import('vite').Plugin}
 */
export function tsgoDts({ tsconfig, srcDir, outDir, noCheck = true }) {
  const declDir = path.join(outDir, 'esm');
  return {
    name: 'pivox:tsgo-dts',
    async closeBundle() {
      const tsgo = await resolveTsgoPath();
      const args = [
        '-p', tsconfig,
        '--emitDeclarationOnly',
        '--declaration',
        '--noEmit', 'false',
        // Overrides for the leaf tsconfig's build-time settings that
        // would otherwise fight a one-shot emit.
        '--declarationMap', 'false',
        '--composite', 'false',
        '--incremental', 'false',
        '--outDir', declDir,
        '--rootDir', srcDir,
      ];
      if (noCheck) args.push('--noCheck');

      const start = performance.now();
      const res = spawnSync(tsgo, args, { stdio: 'inherit' });
      if (res.status !== 0) {
        throw new Error(`[pivox:tsgo-dts] tsgo exited with ${res.status}`);
      }

      // Rewrite `@/*` → relative + append `.js` (resolveFullPaths). Operates
      // on the emitted files on disk; compiler-agnostic, TS7-safe.
      await replaceTscAliasPaths({
        configFile: tsconfig,
        outDir: declDir,
        resolveFullPaths: true,
      });
      const ms = Math.round(performance.now() - start);
      // eslint-disable-next-line no-console
      console.log(`[pivox:tsgo-dts] declarations emitted in ${ms}ms`);
    },
  };
}
