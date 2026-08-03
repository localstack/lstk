import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { describe, expect, test } from "vitest";
import {
  docker,
  dockerIsAvailable,
  lstk,
  requirement,
  tempHome,
  unreachableDockerHost,
  useExclusiveEmulator,
} from "../support/index.ts";
import { fakeBinary } from "../support/fake-binary.ts";
import { privateEmulator, startStubEmulator } from "../support/emulator-stub.ts";

// Ported from test/integration/aws_cmd_test.go.
//
// `lstk aws` discovers a running AWS emulator purely by the container named
// "localstack-aws" being in Docker's "running" state (internal/container's
// name-based lookup, tried before the image/port fallback) -- no image
// content, health check, or license check is involved. So a placeholder
// container (a bare `alpine:latest sleep infinity`) started directly through
// the `docker` CLI is enough to make `lstk aws` treat an emulator as present
// and exercise the real behaviour under test here: argument forwarding,
// endpoint/credential injection, and exit-code propagation. The wrapped `aws`
// itself is always the fake binary from support/fake-binary.ts -- the real AWS
// CLI is never installed or invoked.
//
// Telemetry assertions in the Go tests (assertCommandTelemetry) are dropped:
// which analytics event fired is an internal detail, not something a user of
// `lstk aws` observes.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

const AWS_CONTAINER = "localstack-aws";

/**
 * argv as recorded by the fake tool, with the resolved endpoint URL replaced by a
 * placeholder. The host lstk resolves depends on what DNS answers on the machine,
 * so snapshotting it verbatim would be machine-specific — while the shape, order
 * and completeness of the argv (what these tests are actually about) is not. Tests
 * that care about the port assert it separately.
 */
function stableArgv(args: string[] | undefined): string[] {
  return (args ?? []).map((arg) => (/^https?:\/\//.test(arg) ? "<endpoint>" : arg));
}

async function writeAWSProfile(homeDir: string): Promise<void> {
  const awsDir = path.join(homeDir, ".aws");
  await mkdir(awsDir, { recursive: true });
  await writeFile(
    path.join(awsDir, "config"),
    "[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://localhost.localstack.cloud:4566\n",
  );
  await writeFile(
    path.join(awsDir, "credentials"),
    "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
  );
}

describe("lstk aws without a reachable daemon", () => {
  test("--help and -h skip Docker and the emulator check entirely", async () => {
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome({ env: { DOCKER_HOST: unreachableDockerHost } });

    for (const args of [["--help"], ["-h"], ["s3", "--help"], ["help"], ["s3", "help"]]) {
      const run = await lstk(["aws", ...args], { home, env: { PATH: aws.path } });

      expect(run, `lstk aws ${args.join(" ")}`).toSucceed();
      const call = await aws.lastCall();
      expect(call?.args).toEqual(args);
      expect(call?.args).not.toContain("--endpoint-url");
    }
  });

  test("fails with a clear message when Docker itself is not running", async () => {
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome({ env: { DOCKER_HOST: unreachableDockerHost } });

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

    // Deliberately not snapshotted: this message tailors its suggested start
    // commands to whichever runtimes it detects (`podman machine start`,
    // `open -a Docker`, `rdctl start`, ...), so the full text differs per machine
    // — see the runtime-discovery notes in the repo's CLAUDE.md.
    expect(run).toExitWith(1);
    expect(run).toPrint("Docker is not available");
    // The endpoint is echoed back so the user can see what was tried. Only the
    // distinctive tail is asserted: the scheme and separators differ per platform
    // (unix:// socket path vs npipe://./pipe/...).
    expect(run, "the unreachable endpoint is named so the user can see what was tried").toPrint(
      "nonexistent-lstk-test",
    );
    expect(await aws.calls(), "aws must never be invoked once Docker is unreachable").toEqual([]);
  });

  test("fails with install instructions when the aws CLI is not on PATH", async () => {
    const home = await tempHome();

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: "" } });

    expect(run).toExitWith(1);
    expect(run.stdout).toPrintExactly(`
      Error: aws CLI not found in PATH
        ==> Install AWS CLI: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html
    `);
  });
});

describe.skipIf(noDocker)("lstk aws with a running emulator", () => {
  // Each test below only needs "an emulator is running" under some name, not
  // specifically the canonical `localstack-aws` one, so privateEmulator()
  // gives it a container and config no other test shares — no machine-wide
  // lock needed. The two tests that genuinely need the canonical name and
  // shared identity (the default-port-with-no-config case, and the image/port
  // discovery fallback) live in the locked describe below.

  describe("with no emulator of its own running", () => {
    // Holds the exclusive lock even though it starts nothing: a private tag only
    // makes the container *name* unique, and lstk falls back to matching any
    // known localstack image exposing port 4566 when that name is absent
    // (internal/container/running.go). A concurrent fallback test would
    // otherwise make this one see an emulator that is not its own.
    useExclusiveEmulator();

    test("fails with a clear message when no emulator is running", async () => {
      // No stub is started for emu.name, and the surrounding lock keeps any
      // image/port-fallback test from standing in for it.
      const aws = await fakeBinary({ name: "aws" });
      const emu = privateEmulator();
      const home = await tempHome();
      await home.writeConfig(emu.config);

      const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

      // Snapshotted rather than substring-matched: the promise here is the whole
      // error UX — it names the emulator and offers a way forward — so the diff a
      // reviewer sees on a copy change is exactly what users will read.
      expect(run).toExitWith(1);
      expect(run.stderr, "the failure is rendered through the sink, not raw on stderr").toBe("");
      expect(run.stdout).toPrintExactly(`
        Error: LocalStack AWS Emulator is not running
          ==> Start LocalStack: lstk
          ==> See help: lstk -h
      `);
      expect(await aws.calls(), "aws must never be invoked when nothing is running").toEqual([]);
    });
  });

  test("injects the endpoint and forwards args unchanged", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

    expect(run).toSucceed();
    expect(stableArgv((await aws.lastCall())?.args)).toEqual([
      "--endpoint-url",
      "<endpoint>",
      "s3",
      "ls",
    ]);
  });

  test("uses the port configured in config.toml", async () => {
    const emu = privateEmulator("aws", { port: "4599" });
    await startStubEmulator(emu.name);
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

    expect(run).toSucceed();
    expect((await aws.lastCall())?.args[1]).toContain(":4599");
  });

  test("strips lstk's own flags from passthrough and uses the localstack profile", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();
    await writeAWSProfile(home.path);
    // An explicit --config path, distinct from the home's own (unwritten,
    // default) resolved config file, proves the flag is consumed by lstk
    // itself rather than forwarded. It carries the private emulator's config
    // so the started stub is discovered.
    const configPath = path.join(home.path, "custom-config.toml");
    await writeFile(configPath, emu.config);

    const run = await lstk(["--config", configPath, "--non-interactive", "aws", "s3", "ls"], {
      home,
      env: { PATH: aws.path },
    });

    expect(run).toSucceed();
    expect(stableArgv((await aws.lastCall())?.args)).toEqual([
      "--endpoint-url",
      "<endpoint>",
      "--profile",
      "localstack",
      "s3",
      "ls",
    ]);
  });

  test("injects env credentials when no aws profile exists", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["aws", "sts", "get-caller-identity"], { home, env: { PATH: aws.path } });

    expect(run).toSucceed();
    expect((await aws.lastCall())?.env).toMatchObject({
      AWS_ACCESS_KEY_ID: "test",
      AWS_SECRET_ACCESS_KEY: "test",
      AWS_DEFAULT_REGION: "us-east-1",
    });
  });

  test("respects credentials the user already has set", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["aws", "s3", "ls"], {
      home,
      env: {
        PATH: aws.path,
        AWS_ACCESS_KEY_ID: "custom-key",
        AWS_SECRET_ACCESS_KEY: "custom-secret",
        AWS_DEFAULT_REGION: "eu-west-1",
      },
    });

    expect(run).toSucceed();
    expect(
      (await aws.lastCall())?.env,
      "the user's own credentials must reach the tool untouched",
    ).toMatchObject({
      AWS_ACCESS_KEY_ID: "custom-key",
      AWS_SECRET_ACCESS_KEY: "custom-secret",
      AWS_DEFAULT_REGION: "eu-west-1",
    });
  });

  test("uses the profile instead of injected credentials when one exists", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();
    await home.writeConfig(emu.config);
    await writeAWSProfile(home.path);

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

    expect(run).toSucceed();
    const call = await aws.lastCall();
    expect(call?.args).toContain("--profile");
    expect(call?.env.AWS_ACCESS_KEY_ID).not.toBe("test");
  });

  test("hints at `lstk setup aws` when no profile is configured", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

    expect(run).toSucceed();
    expect(run.stdout).toPrintExactly("> Note: No AWS profile found, run 'lstk setup aws'");
  });

  test("suppresses the setup hint once a profile exists", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();
    await home.writeConfig(emu.config);
    await writeAWSProfile(home.path);

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

    expect(run).toSucceed();
    expect(run.stdout, "no hint, and nothing else added around the tool's output")
      .toPrintExactly("");
  });

  test("propagates the wrapped tool's exit code and stderr", async () => {
    const emu = privateEmulator();
    await startStubEmulator(emu.name);
    const aws = await fakeBinary({
      name: "aws",
      responses: [{ exitCode: 42, stderr: "aws: error: simulated failure" }],
    });
    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

    expect(run).toExitWith(42);
    expect(run.stderr).toPrintExactly("aws: error: simulated failure");
  });
});

describe.skipIf(noDocker)("lstk aws with a running emulator (canonical name)", () => {
  // Both tests below depend on the shared, config-derived `localstack-aws`
  // container name rather than a private one, so they keep the machine-wide
  // lock instead of privateEmulator():
  //
  // - the default-port assertion is specifically about what happens with *no*
  //   config file at all, which resolves to tag "latest" and the canonical name;
  // - the image/port fallback matches any running container tagged as a known
  //   `localstack/*` image on the internal port, which can cross-match another
  //   test's container regardless of name -- see support/emulator-stub.ts.
  useExclusiveEmulator();

  test("uses the default port (4566) when no config overrides it", async () => {
    await startStubEmulator(AWS_CONTAINER);
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

    expect(run).toSucceed();
    expect((await aws.lastCall())?.args[1]).toContain(":4566");
  });

  test("discovers an externally-started container by image and port, not just by name", async () => {
    const fakeImage = "localstack/localstack-pro:e2e-test-fake";
    await docker.pull("alpine:latest");
    await docker.tag("alpine:latest", fakeImage);
    // The external container is named "localstack-main", not AWS_CONTAINER.
    await startStubEmulator("localstack-main", { image: fakeImage, dockerArgs: ["-p", "4566"] });
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();

    const run = await lstk(["aws", "s3", "ls"], { home, env: { PATH: aws.path } });

    expect(run).toSucceed();
    expect((await aws.lastCall())?.args[1]).toMatch(/^https?:\/\//);
  });
});
