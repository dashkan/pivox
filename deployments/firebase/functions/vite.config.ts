import { defineConfig } from "vite";

export default defineConfig({
  build: {
    ssr: "src/index.ts",
    outDir: "lib",
    rolldownOptions: {
      external: [
        "firebase-admin",
        /^firebase-functions/,
        "google-auth-library",
      ],
      output: { entryFileNames: "index.js" },
    },
    sourcemap: true,
    minify: false,
  },
  define: {
    __DEV__: JSON.stringify(process.env.DEV !== "false"),
  },
});
