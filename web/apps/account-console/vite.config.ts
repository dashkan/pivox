import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Builds the account console into the Keycloak theme's account resources.
// Keycloak serves these from a hashed, version-dependent resourceUrl, so:
//   - base "./" keeps any asset URLs relative (no hardcoded absolute paths),
//   - inlineDynamicImports emits a SINGLE js file (no chunk fetches whose URLs
//     would need the unknowable resourceUrl prefix),
//   - fixed entry/asset names let theme/index.ftl reference them deterministically
//     via ${resourceUrl}/app/index.js and ${resourceUrl}/app/assets/index.css.
// CSS url() refs (fonts) stay relative to the CSS file, which resolves correctly
// under resourceUrl since the CSS and its assets ship in the same app/ dir.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: "./",
  server: { host: "127.0.0.1", port: 3002 },
  build: {
    outDir: "../../packages/keycloak-theme/theme/pivox/account/resources/app",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        inlineDynamicImports: true,
        entryFileNames: "index.js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
});
