import { globalIgnores } from "eslint/config";
import rootConfig from "../../eslint.config.js";

export default [
  ...rootConfig,
  globalIgnores([
    "**/node_modules",
    "**/dist",
    "**/.output",
    "**/routeTree.gen.ts",
  ]),
];
