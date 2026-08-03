import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { describe, expect, onTestFinished, test } from "vitest";
import { installExtension } from "../support/extension-fixture.ts";
import { lstk, parseEnvelope, tempHome } from "../support/index.ts";

// Ported from test/integration/json_envelope_test.go, plus the remaining
// (non-PTY) cases from test/integration/json_flag_test.go not already covered
// by tests/json-flag.test.ts (which ports the "status --json" and bare-root
// NOT_JSON_CAPABLE cases). See json-envelope.pty.test.ts for the one case that
// needs a terminal.

// A directory that is guaranteed not to exist, used to override PATH so a
// proxied tool (aws/terraform/cdk/sam) is never actually found — proving lstk
// attempted to invoke it (and thus that --json was forwarded, not intercepted)
// without depending on whether the host happens to have any of them installed.
const emptyPath = "/nonexistent-lstk-e2e-path";

describe("--json envelope rendering", () => {
  test("a command that never opts in renders NOT_JSON_CAPABLE (login)", async () => {
    const home = await tempHome();

    const run = await lstk(["login", "--json"], { home });

    expect(run).toExitWith(1);
    const envelope = parseEnvelope(run.stdout);
    expect(envelope).toMatchObject({
      command: "login",
      status: "error",
      error: { code: "NOT_JSON_CAPABLE", category: "USAGE" },
    });
  });

  test("a usage error after --json was parsed renders as an envelope", async () => {
    const home = await tempHome();

    // --json precedes the unknown flag, so it has already been parsed by the
    // time pflag fails on --bogus-flag.
    const run = await lstk(["stop", "--json", "--bogus-flag"], { home });

    expect(run).toExitWith(1);
    const envelope = parseEnvelope(run.stdout);
    expect(envelope).toMatchObject({
      status: "error",
      error: { code: "USAGE_ERROR", category: "USAGE" },
    });
  });

  test("a usage error before --json was parsed falls back to plain text", async () => {
    const home = await tempHome();

    // --bogus-flag fails before pflag ever reaches --json, so no envelope can
    // be rendered yet — this must fall back to Cobra's plain-text usage error.
    const run = await lstk(["stop", "--bogus-flag", "--json"], { home });

    expect(run).toExitWith(1);
    expect(run.stdout, "no JSON should be attempted when --json wasn't parsed yet").toBe("");
    expect(run).toPrint("bogus-flag");
  });

  test("a malformed config file renders CONFIG_INVALID as an envelope", async () => {
    const home = await tempHome();
    // Malformed TOML (unbalanced brackets) fails to parse in PreRunE, before
    // stop's RunE (and therefore before its EnvelopeSink) ever runs.
    await home.writeConfig('[[containers]\ntype = "aws"\n');

    const run = await lstk(["stop", "--json"], { home });

    expect(run).toExitWith(1);
    expect(run.stderr, "the plain-text fallback in Execute() must not also fire alongside the envelope").toBe(
      "",
    );
    const envelope = parseEnvelope(run.stdout);
    expect(envelope).toMatchObject({
      command: "stop",
      status: "error",
      error: { code: "CONFIG_INVALID", category: "CONFIG", retryable: false },
    });
  });

  test("a missing --config path renders CONFIG_NOT_FOUND as an envelope", async () => {
    const home = await tempHome();
    const missingConfig = `${home.path}/does-not-exist.toml`;

    const run = await lstk(["--config", missingConfig, "reset", "--force", "--json"], { home });

    expect(run).toExitWith(1);
    expect(run.stderr, "the plain-text fallback in Execute() must not also fire alongside the envelope").toBe(
      "",
    );
    const envelope = parseEnvelope(run.stdout);
    expect(envelope).toMatchObject({
      command: "reset",
      status: "error",
      error: { code: "CONFIG_NOT_FOUND", category: "CONFIG" },
    });
  });
});

describe("--json forwarding to proxy commands", () => {
  // aws/terraform/cdk/sam only: az needs a project-local config plus a
  // completed `lstk setup azure` before it will even attempt to invoke the
  // real `az` binary, which in turn needs a running emulator to set up for
  // real — out of scope for what is otherwise a PATH-isolation test. See the
  // report for this session for the az sub-case dropped from the Go table.
  const proxies = [
    { name: "aws", args: ["s3", "ls"] },
    { name: "terraform", args: ["version"] },
    { name: "cdk", args: ["synth"] },
    { name: "sam", args: ["build"] },
  ];

  describe("--json right after the command name is forwarded, not intercepted", () => {
    test.each(proxies)("$name", async ({ name, args }) => {
      const home = await tempHome({ env: { PATH: emptyPath } });

      const run = await lstk([name, "--json", ...args], { home });

      expect(run).toFail();
      expect(run).toPrint("not found in PATH");
      expect(run, "--json should have been forwarded, not rejected by lstk").not.toPrint(
        "is not able to provide output in JSON format",
      );
    });
  });

  describe("--json after the wrapped tool's own action is forwarded, not intercepted", () => {
    test.each(proxies)("$name", async ({ name, args }) => {
      const home = await tempHome({ env: { PATH: emptyPath } });

      const run = await lstk([name, ...args, "--json"], { home });

      expect(run).toFail();
      expect(run).toPrint("not found in PATH");
      expect(run).not.toPrint("is not able to provide output in JSON format");
    });
  });

  describe("--json before the command name is rejected like an unsupported built-in", () => {
    test.each(proxies)("$name", async ({ name, args }) => {
      const home = await tempHome({ env: { PATH: emptyPath } });

      const run = await lstk(["--json", name, ...args], { home });

      expect(run).toExitWith(1);
      const envelope = parseEnvelope(run.stdout);
      expect(envelope).toMatchObject({ command: name, error: { code: "NOT_JSON_CAPABLE" } });
      expect(run.stderr).toBe("");
    });
  });

  describe("boolean-valued --json before the command name (using aws)", () => {
    test("--json=true is rejected", async () => {
      const home = await tempHome({ env: { PATH: emptyPath } });

      const run = await lstk(["--json=true", "aws", "s3", "ls"], { home });

      expect(run).toExitWith(1);
      const envelope = parseEnvelope(run.stdout);
      expect(envelope).toMatchObject({ command: "aws", error: { code: "NOT_JSON_CAPABLE" } });
    });

    test("--json=false is not rejected: the wrapped tool runs", async () => {
      const home = await tempHome({ env: { PATH: emptyPath } });

      const run = await lstk(["--json=false", "aws", "s3", "ls"], { home });

      expect(run).toFail();
      expect(run).toPrint("not found in PATH");
      expect(run).not.toPrint("is not able to provide output in JSON format");
    });

    test("a malformed value is rejected", async () => {
      const home = await tempHome({ env: { PATH: emptyPath } });

      const run = await lstk(["--json=notabool", "aws", "s3", "ls"], { home });

      expect(run).toExitWith(1);
      const envelope = parseEnvelope(run.stdout);
      expect(envelope).toMatchObject({ command: "aws", error: { code: "NOT_JSON_CAPABLE" } });
    });
  });
});

async function tempExtDir(): Promise<string> {
  const dir = await mkdtemp(path.join(os.tmpdir(), "lstk-e2e-ext-"));
  onTestFinished(async () => {
    await rm(dir, { recursive: true, force: true });
  });
  return dir;
}

describe("extensions receive --json via their runtime context, not argv", () => {
  test("--json is consumed by lstk and conveyed as JSON=true, not forwarded", async () => {
    const extDir = await tempExtDir();
    await installExtension(extDir, "hello");
    const home = await tempHome({ env: { PATH: `${extDir}${path.delimiter}${process.env.PATH ?? ""}` } });

    const run = await lstk(["--json", "hello", "--foo"], { home });

    expect(run).toSucceed();
    expect(run).toPrint("ARGS=[--foo]");
    expect(run).toPrint("JSON=true");
    // --json forces non-interactive rendering, so the extension sees that too.
    expect(run).toPrint("NON_INTERACTIVE=true");
  });

  test("without --json, the extension sees JSON=false", async () => {
    const extDir = await tempExtDir();
    await installExtension(extDir, "hello");
    const home = await tempHome({ env: { PATH: `${extDir}${path.delimiter}${process.env.PATH ?? ""}` } });

    const run = await lstk(["hello", "--foo"], { home });

    expect(run).toSucceed();
    expect(run).toPrint("JSON=false");
  });
});
