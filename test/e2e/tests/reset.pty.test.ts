import http from "node:http";
import { describe, expect, test } from "vitest";
import { onTestFinished } from "vitest";
import {
  dockerIsAvailable,
  lstk,
  lstkPty,
  parseEnvelope,
  requirement,
  tempHome,
  useExclusiveEmulator,
  type Home,
} from "../support/index.ts";
import { privateEmulator, startStubEmulator } from "../support/emulator-stub.ts";

// Ported from test/integration/reset_test.go.
//
// `lstk reset` calls the emulator's HTTP reset endpoint directly (resolved via
// LOCALSTACK_HOST), so — like logs — this never needs a real emulator
// container: a stand-in under a privateEmulator() identity satisfies the
// "is it running" check, and a local mock HTTP server stands in for the
// endpoint itself. See support/emulator-stub.ts and the README's "assert
// behaviour, not mechanism".
//
// Named .pty.test.ts because the interactive confirm/cancel case needs a
// real terminal.
//
// Dropped: TestResetTelemetryEmitted / TestResetTelemetryOnFailure assert only
// against a mock analytics server — mechanism, not something a user observes.
// The behaviour they piggyback on (reset succeeding / failing) is already
// covered by the tests below.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

interface ResetServer {
  /** Value for LOCALSTACK_HOST: overrides where `lstk reset` sends the reset call. */
  readonly host: string;
  requestCount(): number;
}

/** A stand-in for the emulator's `/_localstack/state/reset` endpoint. */
async function mockResetServer(status: number): Promise<ResetServer> {
  let count = 0;
  const server = http.createServer((req, res) => {
    if (req.method === "POST" && req.url === "/_localstack/state/reset") {
      count++;
      res.writeHead(status).end();
      return;
    }
    res.writeHead(404).end();
  });

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  onTestFinished(() => new Promise<void>((resolve) => server.close(() => resolve())));

  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("mock reset server did not bind to a TCP port");
  }
  return { host: `127.0.0.1:${address.port}`, requestCount: () => count };
}

/** An isolated home whose config targets `emulator` and whose reset calls go to `resetHost`. */
async function homeTargeting(resetHost: string, emulatorConfig: string): Promise<Home> {
  const home = await tempHome({ env: { LOCALSTACK_HOST: resetHost } });
  await home.writeConfig(emulatorConfig);
  return home;
}

// None of the tests below assert anything tied to the canonical `localstack-aws`
// name — reset only needs "an emulator is running" (or deliberately not) plus a
// reachable reset endpoint — so each gets its own privateEmulator() identity
// instead of the machine-wide lock.
describe.skipIf(noDocker)("lstk reset", () => {
  test("resets emulator state with --force", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const server = await mockResetServer(200);
    const home = await homeTargeting(server.host, emu.config);

    const run = await lstk(["--non-interactive", "reset", "--force"], { home });

    expect(run).toSucceed();
    expect(run.stdout).toPrintExactly(`
      Resetting state......
      ✔︎ Emulator state reset
    `);
    expect(server.requestCount(), "reset endpoint should be called exactly once").toBe(1);
  });

  test("fails without --force in non-interactive mode, and never calls the reset endpoint", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const server = await mockResetServer(200);
    const home = await homeTargeting(server.host, emu.config);

    const run = await lstk(["--non-interactive", "reset"], { home });

    expect(run).toExitWith(1);
    expect(run.stderr).toPrintExactly("Error: reset requires confirmation; use --force to skip in non-interactive mode");
    expect(server.requestCount(), "confirmation must gate the call").toBe(0);
  });

  describe("with no emulator of its own running", () => {
    // Holds the exclusive lock even though it starts nothing: a private tag only
    // makes the container *name* unique, and lstk falls back to matching any
    // known localstack image exposing port 4566 when that name is absent
    // (internal/container/running.go). A concurrent fallback test would
    // otherwise make this one see an emulator that is not its own.
    useExclusiveEmulator();

    test("fails with 'not running' when the emulator isn't up", async () => {
      // No stub is started for emu.name, and the surrounding lock keeps any
      // image/port-fallback test from standing in for it.
      const emu = privateEmulator();
      const home = await tempHome();
      await home.writeConfig(emu.config);

      const run = await lstk(["--non-interactive", "reset", "--force"], { home });

      expect(run).toExitWith(1);
      expect(run.stderr, "the failure is rendered through the sink, not raw on stderr").toBe("");
      expect(run.stdout).toPrintExactly(`
        Error: LocalStack is not running
          ==> Start LocalStack: lstk
          ==> See help: lstk -h
      `);
    });
  });

  test("fails when the reset endpoint itself errors", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const server = await mockResetServer(500);
    const home = await homeTargeting(server.host, emu.config);

    const run = await lstk(["--non-interactive", "reset", "--force"], { home });

    expect(run).toExitWith(1);
    expect(run.stdout).toPrintExactly("Resetting state......");
    expect(run.stderr).toPrintExactly("Error: reset state: LocalStack returned status 500");
  });

  describe("interactive confirmation", () => {
    test("resets when the user confirms with y", async () => {
      const emu = privateEmulator();
      await startStubEmulator(emu.name);
      const server = await mockResetServer(200);
      const home = await homeTargeting(server.host, emu.config);

      const term = lstkPty(["reset"], { home });
      await term.waitFor("Reset emulator state?");
      term.type("y");

      expect(await term.exitCode(), term.output()).toBe(0);
      expect(term.output()).toContain("Emulator state reset");
      expect(server.requestCount(), "reset should be called after confirmation").toBe(1);
    });

    test("cancels when the user presses n", async () => {
      const emu = privateEmulator();
      await startStubEmulator(emu.name);
      const server = await mockResetServer(200);
      const home = await homeTargeting(server.host, emu.config);

      const term = lstkPty(["reset"], { home });
      await term.waitFor("Reset emulator state?");
      term.type("n");

      expect(await term.exitCode(), term.output()).toBe(0);
      expect(term.output()).toContain("Cancelled");
      expect(server.requestCount(), "reset must not be called on cancel").toBe(0);
    });
  });

  describe("--json", () => {
    interface ResetData {
      emulator: { type: string; name: string };
      reset: boolean;
    }

    test("succeeds and reports the reset in the envelope", async () => {
      const emu = privateEmulator();
      await startStubEmulator(emu.name);
      const server = await mockResetServer(200);
      const home = await homeTargeting(server.host, emu.config);

      const run = await lstk(["reset", "--force", "--json"], { home });

      expect(run).toSucceed();
      expect(server.requestCount()).toBe(1);

      const envelope = parseEnvelope<ResetData>(run.stdout);
      // The envelope's real "status" values are "ok" | "error" (see
      // docs/structured-output.md); support/envelope.ts types it as
      // "success" | "error" instead, so this is checked as a plain string.
      expect(envelope.status as string).toBe("ok");
      expect(envelope.command).toBe("reset");
      expect(envelope.data?.emulator.type).toBe("aws");
      expect(envelope.data?.reset).toBe(true);
    });

    test("requires confirmation, as a CONFIRMATION_REQUIRED envelope", async () => {
      const emu = privateEmulator();
      await startStubEmulator(emu.name);
      const home = await tempHome();
      await home.writeConfig(emu.config);

      const run = await lstk(["reset", "--json"], { home });

      expect(run).toExitWith(3);
      const envelope = parseEnvelope(run.stdout);
      expect(envelope.status as string).toBe("error");
      expect(envelope.error?.code).toBe("CONFIRMATION_REQUIRED");
      expect(envelope.error?.category).toBe("USAGE");
      expect(envelope.error?.retryable).toBe(false);
    });

    test("reports EMULATOR_NOT_CONFIGURED when the configured type isn't AWS", async () => {
      const home = await tempHome();
      await home.writeConfig(`[[containers]]\ntype = "snowflake"\ntag = "latest"\nport = "4566"\n`);

      const run = await lstk(["reset", "--force", "--json"], { home });

      expect(run).toExitWith(1);
      const envelope = parseEnvelope(run.stdout);
      expect(envelope.status as string).toBe("error");
      expect(envelope.error?.code).toBe("EMULATOR_NOT_CONFIGURED");
      expect(envelope.error?.category).toBe("EMULATOR");
    });
  });
});
