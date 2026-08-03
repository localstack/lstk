import { describe, expect, test } from "vitest";
import { lstk, tempHome } from "../support/index.ts";

// Ported from test/integration/exit_code_test.go.
//
// Guards against regressions of DEVX-941, where lstk exited 0 even when a
// command failed: an unknown flag, and an unknown subcommand of a parent that
// only groups subcommands (config/setup/volume/snapshot/completion) used to
// exit 0 because Cobra prints help and returns nil in that case.

/**
 * Returns only the lines a reader needs to confirm real help text was printed
 * (the short description and the `Usage:` line), dropping the per-parent
 * subcommand list below it. That list changes whenever a subcommand is added to
 * config/setup/volume/snapshot/completion — churn unrelated to exit codes — so
 * asserting the full dump would make this file fail on changes that belong to
 * other commands.
 */
function shortAndUsage(stdout: string): string {
  const lines = stdout.split("\n");
  const usageIndex = lines.findIndex((line) => line.startsWith("Usage:"));
  return lines.slice(0, usageIndex + 1).join("\n");
}

describe("invalid usage exits non-zero", () => {
  test.each([
    { what: "an unknown flag on start", args: ["start", "--bogus-flag-xyz"] },
    { what: "an unknown flag on the root", args: ["--bogus-flag-xyz"] },
  ])("$what", async ({ args }) => {
    const home = await tempHome();

    const run = await lstk(args, { home });

    expect(run).toExitWith(1);
    expect(run.stderr).toPrintExactly("Error: unknown flag: --bogus-flag-xyz");
  });

  test("an unknown top-level command, which offers help", async () => {
    const home = await tempHome();

    const run = await lstk(["bogus-command"], { home });

    expect(run).toExitWith(1);
    expect(run.stderr).toPrintExactly(`
      Error: unknown command "bogus-command" for lstk
        ==> See help: lstk -h
    `);
  });

  // Note the asymmetry with the case above: an unknown subcommand of a grouping
  // parent gets no "See help" follow-up, so the user is left without a next step.
  test.each([
    { parent: "config", sub: "bogus" },
    { parent: "config", sub: "profile" }, // removed subcommand: must not resurface as a no-op
    { parent: "setup", sub: "bogus" },
    { parent: "volume", sub: "bogus" },
    { parent: "snapshot", sub: "bogus" },
    { parent: "completion", sub: "bogus" },
  ])("an unknown subcommand: lstk $parent $sub", async ({ parent, sub }) => {
    const home = await tempHome();

    const run = await lstk([parent, sub], { home });

    expect(run).toExitWith(1);
    expect(run.stderr).toPrintExactly(`Error: unknown command "${sub}" for "lstk ${parent}"`);
  });
});

describe("a bare subcommand-grouping parent exits zero", () => {
  test.each([
    {
      parent: "config",
      help: `
        Manage configuration

        Usage: lstk config [flags]
      `,
    },
    {
      parent: "setup",
      help: `
        Set up emulator CLI integration for AWS or Azure.

        Usage: lstk setup [flags]
      `,
    },
    {
      parent: "volume",
      help: `
        Manage emulator volume

        Usage: lstk volume [flags]
      `,
    },
    {
      parent: "snapshot",
      help: `
        Manage emulator snapshots

        Usage: lstk snapshot [flags]
      `,
    },
    {
      parent: "completion",
      help: `
        Generate the autocompletion script for lstk for the specified shell.
        See each sub-command's help for details on how to use the generated script.

        Usage: lstk completion [flags]
      `,
    },
  ])("lstk $parent prints its help", async ({ parent, help }) => {
    const home = await tempHome();

    const run = await lstk([parent], { home });

    expect(run).toSucceed();
    expect(shortAndUsage(run.stdout)).toPrintExactly(help);
  });
});
