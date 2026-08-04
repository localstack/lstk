import { describe, expect, test } from "vitest";
import {
  authToken,
  docker,
  dockerIsAvailable,
  lstk,
  requireAuthToken,
  requirement,
  tempHome,
  useExclusiveEmulator,
} from "../support/index.ts";
import { privateEmulator, startStubEmulator } from "../support/emulator-stub.ts";

// Ported from test/integration/stop_test.go and test/integration/restart_test.go.
//
// `lstk stop`/`restart` discover a running emulator by container name first,
// falling back to (known image repo, internal port) for a container started
// outside lstk. Most cases here only need that discovery and the messaging
// around it, so they use a placeholder container (`sleep infinity` on a plain
// image) instead of a real, license-gated emulator — see
// support/emulator-stub.ts. Telemetry emission is internal mechanism and is
// not asserted here; see README "Assert behaviour, not mechanism".
//
// Most `lstk stop` cases only need "an emulator is running" under some name,
// so they give themselves a private container name/tag via `privateEmulator()`
// and need no lock. A subset genuinely depends on the canonical name, a real
// published port, or a real license-validated emulator; those stay under
// `useExclusiveEmulator()`, with a comment on why each one can't be
// privatized. `lstk restart`'s cases are all in that latter category, so its
// describe block keeps the lock entirely.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

describe.skipIf(noDocker)("lstk stop", () => {
  test("stops a running emulator", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["stop"], { home });

    expect(run).toSucceed();
    expect(run.stdout).toPrintExactly(`
      Stopping LocalStack......
      ✔︎ LocalStack AWS Emulator stopped
    `);
    expect(await docker.containerIsRunning(emu.name)).toBe(false);
  });

  test("reports the emulator-specific not-running message, matching status", async () => {
    const emu = privateEmulator("snowflake");
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["stop"], { home });

    expect(run).toExitWith(1);
    expect(run.stdout).toPrintExactly("Error: LocalStack Snowflake Emulator is not running");
  });

  test("--json reports which emulator was stopped", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["stop", "--json"], { home });

    expect(run).toSucceed();
    const envelope = JSON.parse(run.stdout) as {
      status: string;
      command: string;
      error: unknown;
      data: { emulators: Array<{ type: string; name: string; wasRunning: boolean }> };
    };
    expect(envelope.status).toBe("ok");
    expect(envelope.command).toBe("stop");
    expect(envelope.error).toBeNull();
    expect(envelope.data.emulators).toHaveLength(1);
    expect(envelope.data.emulators[0]).toMatchObject({
      type: "aws",
      name: emu.name,
      wasRunning: true,
    });
  });

  // These cannot use a private emulator identity, so they keep the
  // machine-wide lock instead:
  describe("against the canonical name or a real published port", () => {
    useExclusiveEmulator();

    test("is idempotent: a second stop fails once the emulator is already gone", async () => {
      // A private identity is not enough here. The second stop finds no
      // container under the configured name and falls back to matching (known
      // image repo, port 4566) — so any concurrent test running a real
      // `localstack/*` image on 4566 would answer it, and the second stop would
      // succeed. Asserting an absence for an AWS-typed config needs the lock.
      const emu = privateEmulator();
      await startStubEmulator(emu.name);
      const home = await tempHome();
      await home.writeConfig(emu.config);

      const first = await lstk(["stop"], { home });
      expect(first).toSucceed();

      const second = await lstk(["stop"], { home });
      expect(second).toExitWith(1);
    });

    test("fails with a not-running message when nothing is running", async () => {
      // No config is written at all, so this exercises the true zero-config
      // default (AWS, canonical name `localstack-aws`, port 4566). A real
      // `localstack-aws` started concurrently elsewhere would make "not
      // running" flaky.
      const home = await tempHome();

      const run = await lstk(["stop"], { home });

      expect(run).toExitWith(1);
      expect(run.stdout).toPrintExactly("Error: LocalStack AWS Emulator is not running");
    });

    test("ignores a foreign emulator of a different type occupying the configured port", async () => {
      // Deliberately exercises the image/port discovery fallback: a foreign
      // AWS-image container published on the canonical port 4566 while config
      // targets snowflake, which only the fallback's image-repo matching --
      // not a container name -- disambiguates.
      const fakeAwsImage = "localstack/localstack-pro:test-fake-ignore";
      await docker.pull("alpine:latest");
      await docker.tag("alpine:latest", fakeAwsImage);
      // An AWS-image container sits on port 4566 while config targets snowflake.
      await startStubEmulator("localstack-external-aws", {
        image: fakeAwsImage,
        hostBinding: { hostPort: "4566" },
      });

      const home = await tempHome();
      await home.writeConfig(`[[containers]]\ntype = "snowflake"\ntag = "latest"\nport = "4566"\n`);

      const run = await lstk(["stop"], { home });

      expect(run).toExitWith(1);
      // The exact snapshot below already proves "stopped" never appears; the
      // foreign container's running state is the real assertion of "untouched".
      expect(run.stdout).toPrintExactly("Error: LocalStack Snowflake Emulator is not running");
      expect(
        await docker.containerIsRunning("localstack-external-aws"),
        "the foreign AWS container must be untouched by a snowflake-targeted stop",
      ).toBe(true);
    });

    test("stops a container started outside lstk, discovered by image and port", async () => {
      // Deliberately exercises the image/port discovery fallback: a foreign
      // container tagged as a real `localstack/*` image repo and published on
      // the canonical port 4566.
      const fakeImage = "localstack/localstack-pro:test-fake-external";
      await docker.pull("alpine:latest");
      await docker.tag("alpine:latest", fakeImage);
      await startStubEmulator("localstack-external", {
        image: fakeImage,
        hostBinding: { hostPort: "4566" },
      });

      const home = await tempHome();

      const run = await lstk(["stop"], { home });

      expect(run).toSucceed();
      expect(run.stdout).toPrintExactly(`
        Stopping LocalStack......
        ✔︎ LocalStack AWS Emulator stopped
      `);
      expect(await docker.containerIsRunning("localstack-external")).toBe(false);
    });

    test("--json reports EMULATOR_NOT_RUNNING when nothing is running", async () => {
      // No config is written at all, so this exercises the true zero-config
      // default (AWS, canonical name `localstack-aws`, port 4566), same
      // reasoning as the plain not-running case above.
      const home = await tempHome();

      const run = await lstk(["stop", "--json"], { home });

      expect(run).toExitWith(1);
      const envelope = JSON.parse(run.stdout) as {
        status: string;
        error: { code: string; category: string };
      };
      expect(envelope.status).toBe("error");
      expect(envelope.error.code).toBe("EMULATOR_NOT_RUNNING");
      expect(envelope.error.category).toBe("EMULATOR");
    });
  });
});

describe.skipIf(noDocker)("lstk restart", () => {
  // Every case here either relies on the true zero-config default or starts a
  // real, license-validated emulator via `lstk start` (always under the
  // canonical name) -- neither can use a private stub identity, so the whole
  // block keeps the lock.
  useExclusiveEmulator();

  test("fails with a not-running message when nothing is running", async () => {
    const home = await tempHome();

    const run = await lstk(["restart"], { home });

    expect(run).toExitWith(1);
    expect(run.stdout).toPrintExactly("Error: LocalStack AWS Emulator is not running");
  });

  // The cases below exercise a genuine stop+start cycle (restart = Stop then
  // Start for real), so — unlike the stop tests above — they need a real
  // license-validated emulator, not a placeholder container.
  describe.skipIf(!authToken())("against a real emulator", () => {
    test("stops and restarts a running emulator", async () => {
      const home = await tempHome({ env: { LOCALSTACK_AUTH_TOKEN: requireAuthToken() } });

      const start = await lstk(["start", "--non-interactive"], { home });
      expect(start).toSucceed();

      const run = await lstk(["restart"], { home });

      expect(run).toSucceed();
      expect(run).toPrint("stopped");
      expect(run).toPrint("LocalStack");

      const status = await lstk(["status"], { home });
      expect(status).toPrint("is running");
    });

    test("--persist enables persistence for the restarted instance", async () => {
      const home = await tempHome({ env: { LOCALSTACK_AUTH_TOKEN: requireAuthToken() } });

      const start = await lstk(["start", "--non-interactive"], { home });
      expect(start).toSucceed();

      const run = await lstk(["restart", "--persist"], { home });

      expect(run).toSucceed();
      expect(run).toPrint("• Persistence: Enabled");
    });

    test("without --persist, carries forward persistence from the running instance", async () => {
      const home = await tempHome({ env: { LOCALSTACK_AUTH_TOKEN: requireAuthToken() } });

      const start = await lstk(["start", "--non-interactive", "--persist"], { home });
      expect(start).toSucceed();

      const run = await lstk(["restart"], { home });

      expect(run).toSucceed();
      expect(
        run,
        "restart without --persist must not silently drop a running instance's persistence",
      ).toPrint("• Persistence: Enabled");
    });
  });
});
