/*
 * Compiles the two Tailwind entries (login + account) to each theme
 * type's resources/css/theme.css. One script so the workspace's
 * `web:build` (one-shot) and `web:build:watch` (passes --watch) both work:
 * the --watch flag is forwarded to both Tailwind CLI processes.
 */
import { spawn } from "node:child_process";

const watch = process.argv.includes("--watch");

// Only the login theme is Tailwind-compiled. The account console is a React
// PatternFly SPA recolored with a hand-authored variable-override stylesheet
// (theme/pivox/account/resources/css/pivox-account.css), so it has no Tailwind
// build step.
const entries = [
  { in: "src/login.css", out: "theme/pivox/login/resources/css/theme.css" },
];

const run = ({ in: input, out }) =>
  new Promise((resolve, reject) => {
    const args = ["-i", input, "-o", out, "--minify"];
    if (watch) args.push("--watch");
    const child = spawn("tailwindcss", args, { stdio: "inherit", shell: true });
    child.on("error", reject);
    // In --watch mode the process never exits; resolve on spawn so both
    // watchers run concurrently. In one-shot mode resolve on clean exit.
    if (watch) {
      resolve();
    } else {
      child.on("exit", (code) =>
        code === 0 ? resolve() : reject(new Error(`${input} -> exit ${code}`)),
      );
    }
  });

try {
  await Promise.all(entries.map(run));
} catch (err) {
  console.error(err);
  process.exit(1);
}
