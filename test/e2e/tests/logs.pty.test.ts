import { execa } from "execa";
import { describe, expect, test } from "vitest";
import {
  dockerIsAvailable,
  docker,
  lstk,
  lstkPty,
  requirement,
  tempHome,
  useExclusiveEmulator,
  type Home,
} from "../support/index.ts";
import { lstkBinary } from "../support/binary.ts";
import { privateEmulator, startStubEmulator, writeContainerLogLines } from "../support/emulator-stub.ts";

// Ported from test/integration/logs_test.go.
//
// Discovery matches the running emulator by container name (falling back to
// image matching for a container started outside lstk) — never by what the
// container actually runs — so every test here stands a plain container in
// for a real emulator instead of calling `lstk start`. That keeps the suite
// free of Docker Hub pulls and the license/token flow entirely. See
// support/emulator-stub.ts.
//
// Named .pty.test.ts because the interactive-scrollback case needs a real
// terminal (Bubble Tea's tea.Println can only be observed through a PTY).
//
// Dropped: the two telemetry assertions Go bundles into
// TestLogsExitsByDefault / TestLogsWorksWithExternalContainer
// (assertCommandTelemetry) are mechanism (internal analytics), not something a
// user observes — the behavioural half of each is still ported below.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

const awsConfig = `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\n`;

async function homeWithAwsConfig(): Promise<Home> {
  const home = await tempHome();
  await home.writeConfig(awsConfig);
  return home;
}

describe("lstk logs", () => {
  // No container and no Docker needed: validation runs before any runtime call.
  test("--tail rejects a non-numeric value", async () => {
    const home = await homeWithAwsConfig();

    const run = await lstk(["logs", "--tail", "bogus"], { home });

    expect(run).toExitWith(1);
    expect(run.stderr).toPrintExactly("Error: invalid --tail value \"bogus\": expected a non-negative integer or \"all\"");
  });
});

describe.skipIf(noDocker)("lstk logs with a running emulator", () => {
  // Each test below only needs "an emulator is running" under some name, not
  // specifically the canonical `localstack-aws` one, so privateEmulator()
  // gives it a container and config no other test shares — no machine-wide
  // lock needed. The one test that genuinely needs the canonical name (the
  // image/port discovery fallback below) keeps useExclusiveEmulator().

  test("exits cleanly once the backlog is printed, without --follow", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["logs"], { home });

    expect(run).toSucceed();
  });

  describe("with no emulator of its own running", () => {
    // Holds the exclusive lock even though it starts nothing: a private tag only
    // makes the container *name* unique, and lstk falls back to matching any
    // known localstack image exposing port 4566 when that name is absent
    // (internal/container/running.go). A concurrent fallback test would
    // otherwise make this one see an emulator that is not its own.
    useExclusiveEmulator();

    test("fails with a clear message when the emulator is not running", async () => {
      // No stub is started for emu.name, and the surrounding lock keeps any
      // image/port-fallback test from standing in for it.
      const emu = privateEmulator();
      const home = await tempHome();
      await home.writeConfig(emu.config);

      const run = await lstk(["logs", "--follow"], { home });

      expect(run).toExitWith(1);
      expect(run.stderr, "the failure is rendered through the sink, not raw on stderr").toBe("");
      expect(run.stdout).toPrintExactly(`
        Error: LocalStack AWS Emulator is not running
          ==> Start LocalStack: lstk
          ==> See help: lstk -h
      `);
    });
  });

  // Regression: --tail counts the lines lstk prints, not raw container lines.
  // A burst of filtered request logs after the newest visible line used to
  // consume the whole limit, so `lstk logs --tail 1` printed nothing at all.
  test("--tail counts the lines lstk prints, not the raw container lines it filters out", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const visible = "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : tail-visible-marker";
    const filtered = Array.from(
      { length: 5 },
      (_, i) =>
        `2026-07-07T10:05:${String(12 + i).padStart(2, "0")}.240  INFO --- [et.reactor-0] localstack.request.http : GET /_localstack/tail-filtered-marker => 200`,
    );
    await writeContainerLogLines(emu.name, [visible, ...filtered]);
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["logs", "--tail", "1"], { home });

    expect(run).toSucceed();
    // A single line: proves --tail counted only the visible line, and that the
    // filtered request-log burst never reached output at all.
    expect(run.stdout).toPrintExactly("2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : tail-visible-marker");
  });

  test("--tail / -n limits output to the last N visible lines", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const lines = Array.from({ length: 10 }, (_, i) => `tail-marker-${i + 1}`);
    await writeContainerLogLines(emu.name, lines);
    const home = await tempHome();
    await home.writeConfig(emu.config);

    for (const flag of ["--tail", "-n"]) {
      const run = await lstk(["logs", flag, "3"], { home });
      expect(run).toSucceed();
      for (let i = 8; i <= 10; i++) {
        expect(run, `${flag} 3 should show tail-marker-${i}`).toPrint(`tail-marker-${i}`);
      }
      for (let i = 1; i <= 7; i++) {
        expect(run, `${flag} 3 should cut off tail-marker-${i}`).not.toPrint(`tail-marker-${i}\n`);
      }
    }
  });

  test("shows every line when --tail is not given", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const lines = Array.from({ length: 10 }, (_, i) => `tail-marker-${i + 1}`);
    await writeContainerLogLines(emu.name, lines);
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["logs"], { home });

    expect(run).toSucceed();
    for (let i = 1; i <= 10; i++) {
      expect(run).toPrint(`tail-marker-${i}`);
    }
  });

  test("--follow --tail starts streaming from the tail, not the whole backlog", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const lines = Array.from({ length: 10 }, (_, i) => `tail-marker-${i + 1}`);
    await writeContainerLogLines(emu.name, lines);
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const subprocess = execa(lstkBinary, ["logs", "--follow", "--tail", "3"], {
      cwd: home.path,
      env: home.env,
      extendEnv: false,
      reject: false,
    });

    try {
      const firstMarkerLine = await waitForOutputLine(subprocess, /tail-marker-/, 15_000);
      // The backlog is capped at the last 3 lines, so the first line streamed
      // must be tail-marker-8; an older marker first means --tail was ignored.
      expect(firstMarkerLine).toContain("tail-marker-8");
    } finally {
      subprocess.kill();
      await subprocess;
    }
  });

  test("--follow streams new lines as they are written", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const home = await tempHome();
    await home.writeConfig(emu.config);
    const marker = "lstk-logs-test-marker";

    const subprocess = execa(lstkBinary, ["logs", "--follow"], {
      cwd: home.path,
      env: home.env,
      extendEnv: false,
      reject: false,
    });

    try {
      // Attach the listener before writing anything, so nothing can arrive unobserved.
      const found = waitForOutputLine(subprocess, marker, 15_000);
      // Give lstk logs a moment to attach before generating output.
      await sleep(500);
      await execa("docker", ["exec", emu.name, "sh", "-c", `echo ${marker} >/proc/1/fd/1`]);

      await found;
    } finally {
      subprocess.kill();
      await subprocess;
    }
  });

  // Interactive lstk logs must preserve full scrollback like `docker logs`,
  // not just whatever fit in the TUI's capped history. tea.Println writes log
  // lines permanently above the program instead of into the redrawn frame, so
  // they must all still be present once the run exits.
  test("interactive logs preserve full scrollback, not just the capped TUI history", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const lineCount = 550;
    const lines = Array.from({ length: lineCount }, (_, i) => `tail-marker-${i + 1}`);
    await writeContainerLogLines(emu.name, lines);
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const term = lstkPty(["logs"], { home });
    const exitCode = await term.exitCode();
    expect(exitCode, `lstk logs should exit cleanly:\n${term.output()}`).toBe(0);

    const output = term.output();
    for (let i = 1; i <= lineCount; i++) {
      expect(output, `tail-marker-${i} should survive scrollback`).toContain(`tail-marker-${i}`);
    }
  });
});

describe.skipIf(noDocker)("lstk logs with a container under a different name", () => {
  // Exercises the image/internal-port discovery fallback, which matches any
  // container running a known `localstack/*` image on the internal port —
  // unlike the name-first path, that can cross-match another test's container,
  // so this keeps the machine-wide lock.
  useExclusiveEmulator();

  // lstk must find the emulator even when it is running under a name other
  // than the config-derived canonical one (e.g. started outside lstk),
  // falling back to matching by known image + internal port.
  test("finds the emulator when it was started outside lstk under a different name", async () => {
    await docker.pull("alpine:latest");
    await docker.tag("alpine:latest", "localstack/localstack-pro:logs-e2e-test-fake");
    await startStubEmulator("localstack-main", {
      image: "localstack/localstack-pro:logs-e2e-test-fake",
      dockerArgs: ["-p", "4566"],
    });
    const home = await homeWithAwsConfig();

    const run = await lstk(["logs"], { home });

    expect(run).toSucceed();
  });
});

/** Resolves with the first stdout line matching `needle`, or rejects on timeout. */
function waitForOutputLine(
  subprocess: ReturnType<typeof execa>,
  needle: string | RegExp,
  timeoutMs: number,
): Promise<string> {
  const matches = (line: string) =>
    typeof needle === "string" ? line.includes(needle) : needle.test(line);

  return new Promise<string>((resolve, reject) => {
    let buffer = "";
    const timer = setTimeout(() => {
      reject(new Error(`timed out waiting for ${String(needle)}\n--- output so far ---\n${buffer}`));
    }, timeoutMs);

    subprocess.stdout?.on("data", (chunk: Buffer) => {
      buffer += chunk.toString();
      for (const line of buffer.split("\n")) {
        if (matches(line)) {
          clearTimeout(timer);
          resolve(line);
          return;
        }
      }
    });
  });
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
