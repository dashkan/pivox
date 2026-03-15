import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";
import rootConfig from "../../eslint.config.js";

// Strip plugin definitions from root config to avoid conflicts with
// eslint-config-next (both define the "import" plugin).
const rootRules = rootConfig.filter((c) => !c.plugins);

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  ...rootRules,
  globalIgnores([
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
