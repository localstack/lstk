import { defineConfig, type ViteUserConfig } from "vitest/config";

type Reporters = NonNullable<NonNullable<ViteUserConfig["test"]>["reporters"]>;

const junit: Reporters = process.env.CREATE_JUNIT_REPORT
  ? [["junit", { outputFile: "../../test-e2e-results.xml" }]]
  : [];

export default defineConfig({
  test: {
    globals: true,
    include: ["tests/**/*.test.ts"],
    setupFiles: ["support/matchers.ts"],
    globalSetup: ["support/global-setup.ts"],
    // Starting a real emulator can take a while on a cold image pull.
    testTimeout: 120_000,
    hookTimeout: 120_000,
    // "default" is kept on CI alongside the annotations reporter: on its own,
    // github-actions emits only inline annotations, leaving the job log with no
    // pass/fail/skip summary and forcing anyone diagnosing a run to fetch and parse
    // the JUnit artifact instead of just reading the log.
    reporters: [
      ...(process.env.CI ? (["github-actions", "default"] as const) : (["default"] as const)),
      ...junit,
    ],
  },
});
