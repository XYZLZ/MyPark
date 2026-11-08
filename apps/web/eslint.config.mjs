import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Only proxy.ts refreshes a session (spec 5.3). A second importer would
  // compile a second copy of the single-flight lock.
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/proxy.ts", "src/proxy.test.ts", "src/lib/session/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["**/lib/session/refresh"],
              message: "Only src/proxy.ts may import the session refresher.",
            },
          ],
        },
      ],
    },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
