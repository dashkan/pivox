import { MakerDeb } from '@electron-forge/maker-deb';
import { MakerDMG } from '@electron-forge/maker-dmg';
import { MakerRpm } from '@electron-forge/maker-rpm';
import { MakerSquirrel } from '@electron-forge/maker-squirrel';
import { MakerZIP } from '@electron-forge/maker-zip';
import { AutoUnpackNativesPlugin } from '@electron-forge/plugin-auto-unpack-natives';
import { FusesPlugin } from '@electron-forge/plugin-fuses';
import { VitePlugin } from '@electron-forge/plugin-vite';
import type { ForgeConfig } from '@electron-forge/shared-types';
import { FuseV1Options, FuseVersion } from '@electron/fuses';

const config: ForgeConfig = {
  packagerConfig: {
    asar: true,
    appBundleId: 'app.pivox.desktop',
    // Base name only — packager appends .icns / .ico.
    icon: 'build/icon',
    // Load-bearing: OIDC login runs in the system browser and returns via a
    // `pivox://oidc-callback` deep link (src/main/oidc-login-flow.ts). Without
    // this, the OS never routes the scheme back to us and auth dead-ends.
    protocols: [{ name: 'Pivox', schemes: ['pivox'] }],
    // src/main.ts reads the Linux window icon from process.resourcesPath.
    extraResource: ['resources/icon.png'],
    // Unsigned, as before. build/entitlements.mac.plist is currently
    // UNREFERENCED — Forge only applies it via osxSign. Enabling signing without
    // it yields a hardened-runtime app that crashes when V8 JITs, so wire it then:
    //   osxSign: { optionsForFile: () => ({ entitlements: 'build/entitlements.mac.plist' }) }
    osxSign: undefined,
    osxNotarize: undefined,
    extendInfo: {
      NSCameraUsageDescription:
        "Application requests access to the device's camera.",
      NSMicrophoneUsageDescription:
        "Application requests access to the device's microphone.",
      NSDocumentsFolderUsageDescription:
        "Application requests access to the user's Documents folder.",
      NSDownloadsFolderUsageDescription:
        "Application requests access to the user's Downloads folder.",
    },
  },
  rebuildConfig: {},
  makers: [
    // Replaces electron-builder's NSIS target. electron-squirrel-startup in
    // src/main.ts is this maker's companion.
    new MakerSquirrel({ name: 'pivox', setupIcon: 'build/icon.ico' }),
    new MakerZIP({}, ['darwin']),
    new MakerDMG({ icon: 'build/icon.icns' }, ['darwin']),
    // electron-builder also shipped AppImage + snap; Forge has no maker for
    // either, so Linux coverage is deb + rpm.
    new MakerDeb({
      options: {
        // Debian names must be lowercase; productName is "Pivox".
        name: 'pivox',
        maintainer: 'Pivox',
        homepage: 'https://pivox.app',
        icon: 'build/icon.png',
        categories: ['AudioVideo'],
      },
    }),
    new MakerRpm({
      options: {
        name: 'pivox',
        homepage: 'https://pivox.app',
        icon: 'build/icon.png',
        categories: ['AudioVideo'],
      },
    }),
  ],
  plugins: [
    // No native deps today; a guard if one is ever added. See the native-dep
    // invariant in pnpm-workspace.yaml first.
    new AutoUnpackNativesPlugin({}),
    new VitePlugin({
      build: [
        { entry: 'src/main.ts', config: 'vite.main.config.ts', target: 'main' },
        {
          entry: 'src/preload.ts',
          config: 'vite.preload.config.ts',
          target: 'preload',
        },
      ],
      renderer: [
        {
          // Derives the MAIN_WINDOW_VITE_* globals that src/main.ts reads.
          name: 'main_window',
          // `.mts` because the config imports the ESM-only @pivox/storage, and
          // without `"type": "module"` (which would break the CJS main bundle)
          // Vite loads a `.ts` config via require().
          config: 'vite.renderer.config.mts',
        },
      ],
    }),
    new FusesPlugin({
      version: FuseVersion.V1,
      [FuseV1Options.RunAsNode]: false,
      [FuseV1Options.EnableCookieEncryption]: true,
      [FuseV1Options.EnableNodeOptionsEnvironmentVariable]: false,
      [FuseV1Options.EnableNodeCliInspectArguments]: false,
      [FuseV1Options.EnableEmbeddedAsarIntegrityValidation]: true,
      [FuseV1Options.OnlyLoadAppFromAsar]: true,
    }),
  ],
};

export default config;
