import { execa } from "execa";
import { access, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { afterEach, describe, expect, test } from "vitest";
import {
  docker,
  dockerIsAvailable,
  lstk,
  normalizeCliOutput,
  requirement,
  tempHome,
  useExclusiveEmulator,
} from "../support/index.ts";
import { fakeBinary, type FakeBinary, type FakeCall } from "../support/fake-binary.ts";

// Ported from test/integration/terraform_cmd_test.go.
//
// Like `lstk aws` (see aws-proxy.test.ts), `lstk terraform`/`lstk tf` discover a
// running emulator purely by a container's name and running state, so a
// placeholder `alpine:latest sleep infinity` container is enough to stand in
// for a real emulator here too. The wrapped tool is always the fake binary
// from support/fake-binary.ts, never a real terraform/tofu install.
//
// Unlike `aws`, a *proxied* terraform subcommand (anything but fmt/validate/
// version/init/-help) makes lstk itself call `terraform providers schema
// -json` first, to learn the AWS provider's endpoint attribute names, then
// writes `localstack_providers_override.tf` before invoking the real
// subcommand and removes it after. That override file is this proxy's way of
// "pointing the tool at LocalStack" (aws's equivalent is --endpoint-url), so
// its generation/content/cleanup is very much in scope here.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

const AWS_CONTAINER = "localstack-aws";
const SNOWFLAKE_CONTAINER = "localstack-snowflake";
const OVERRIDE_FILE = "localstack_providers_override.tf";

// A minimal `terraform providers schema -json` payload exposing a couple of
// endpoint attributes for the AWS provider -- enough for lstk's endpoint
// discovery to produce a non-empty `endpoints { ... }` block.
const awsSchemaJSON = JSON.stringify({
  provider_schemas: {
    "registry.terraform.io/hashicorp/aws": {
      provider: {
        block: {
          block_types: {
            endpoints: { block: { attributes: { s3: { type: "string" }, sqs: { type: "string" } } } },
          },
        },
      },
    },
  },
});

/** A fake terraform that answers a provider-schema query and records everything else. */
async function fakeTerraform(options: { name?: string; captureOverride?: boolean } = {}): Promise<FakeBinary> {
  return fakeBinary({
    name: options.name ?? "terraform",
    responses: [{ when: ["providers", "schema"], stdout: awsSchemaJSON }],
    captureFiles: options.captureOverride ? [OVERRIDE_FILE] : [],
  });
}

/**
 * Starts a placeholder container so lstk's name-based "is it running" check
 * matches it. Force-removes any leftover container under the same name first:
 * `useExclusiveEmulator()` only serializes against other e2e test files, not
 * against unrelated Docker users on the same machine (e.g. the Go integration
 * suite, which uses the same container names with no knowledge of this lock).
 */
async function startPlaceholderEmulator(name: string): Promise<void> {
  await docker.pull("alpine:latest");
  await docker.removeContainer(name);
  const result = await execa("docker", ["run", "-d", "--name", name, "alpine:latest", "sleep", "infinity"], {
    reject: false,
  });
  if (result.exitCode !== 0) {
    throw new Error(`docker run --name ${name} failed: ${result.stderr}`);
  }
}

async function fileExists(file: string): Promise<boolean> {
  try {
    await access(file);
    return true;
  } catch {
    return false;
  }
}

/** The "plan"-invoking call, as opposed to the preceding "providers schema" call. */
function planCall(calls: FakeCall[]): FakeCall | undefined {
  return calls.find((c) => c.args.includes("plan"));
}

describe("lstk terraform without an emulator", () => {
  test("forwards args to terraform unchanged", async () => {
    const terraform = await fakeBinary({ name: "terraform" });
    const home = await tempHome();

    const run = await lstk(["terraform", "version"], { home, env: { PATH: terraform.path } });

    expect(run).toSucceed();
    expect((await terraform.lastCall())?.args).toEqual(["version"]);
  });

  test("the tf alias forwards args the same way", async () => {
    const terraform = await fakeBinary({ name: "terraform" });
    const home = await tempHome();

    const run = await lstk(["tf", "version"], { home, env: { PATH: terraform.path } });

    expect(run).toSucceed();
    expect((await terraform.lastCall())?.args).toEqual(["version"]);
  });

  test("LSTK_TF_CMD selects an alternate binary (e.g. OpenTofu)", async () => {
    const tofu = await fakeBinary({ name: "tofu" });
    const home = await tempHome();

    const run = await lstk(["terraform", "version"], {
      home,
      env: { PATH: tofu.path, LSTK_TF_CMD: "tofu" },
    });

    expect(run).toSucceed();
    expect((await tofu.lastCall())?.args).toEqual(["version"]);
  });

  test("propagates the wrapped tool's exit code and stderr", async () => {
    const terraform = await fakeBinary({
      name: "terraform",
      responses: [{ exitCode: 5, stderr: "terraform: simulated failure" }],
    });
    const home = await tempHome();

    const run = await lstk(["terraform", "validate"], { home, env: { PATH: terraform.path } });

    expect(run).toExitWith(5);
    expect(run.stderr).toPrintExactly("terraform: simulated failure");
  });

  test("fails with install instructions when terraform is not on PATH", async () => {
    const home = await tempHome();

    const run = await lstk(["terraform", "version"], { home, env: { PATH: "" } });

    expect(run).toFail();
    expect(run.stdout).toPrintExactly(`
      Error: terraform not found in PATH
        ==> Install Terraform CLI: https://developer.hashicorp.com/terraform/cli
    `);
  });

  test.each(["fmt", "validate", "version", "init"])(
    "unproxied subcommand %s skips schema resolution and the override file, even with --region/--account",
    async (sub) => {
      const terraform = await fakeTerraform();
      const home = await tempHome();

      const run = await lstk(
        ["terraform", "--region", "us-west-2", "--account", "111111111111", sub],
        { home, env: { PATH: terraform.path } },
      );

      expect(run).toSucceed();
      expect((await terraform.lastCall())?.args).toEqual([sub]);
      expect(await fileExists(path.join(home.path, OVERRIDE_FILE))).toBe(false);
    },
  );

  test.each([["--help"], ["-h"], ["-help"], ["plan", "--help"]])(
    "%s is forwarded untouched and never triggers schema resolution",
    async (...args) => {
      const terraform = await fakeTerraform();
      const home = await tempHome();

      const run = await lstk(["terraform", ...args], { home, env: { PATH: terraform.path } });

      expect(run).toSucceed();
      expect((await terraform.lastCall())?.args).toEqual(args);
      expect(await fileExists(path.join(home.path, OVERRIDE_FILE))).toBe(false);
    },
  );

  test("rejects an invalid --account before invoking terraform", async () => {
    const terraform = await fakeBinary({ name: "terraform" });
    const home = await tempHome();

    const run = await lstk(["terraform", "--account", "12345", "plan"], { home, env: { PATH: terraform.path } });

    expect(run).toFail();
    expect(run.stdout).toPrintExactly("Error: --account must be a 12-digit AWS account id, got \"12345\"");
    expect(await terraform.calls()).toEqual([]);
  });

  test("rejects a flag with a missing value before invoking terraform", async () => {
    const terraform = await fakeBinary({ name: "terraform" });
    const home = await tempHome();

    const run = await lstk(["terraform", "--region"], { home, env: { PATH: terraform.path } });

    expect(run).toFail();
    expect(run.stdout).toPrintExactly("Error: --region requires a value");
    expect(await terraform.calls()).toEqual([]);
  });

  test("forwards flags placed after the subcommand", async () => {
    const terraform = await fakeBinary({ name: "terraform" });
    const home = await tempHome();

    const run = await lstk(["terraform", "version", "--region", "us-west-2"], {
      home,
      env: { PATH: terraform.path },
    });

    expect(run).toSucceed();
    expect((await terraform.lastCall())?.args).toEqual(["version", "--region", "us-west-2"]);
  });

  test("rejects a flag placed before the subcommand", async () => {
    const terraform = await fakeBinary({ name: "terraform" });
    const home = await tempHome();

    const run = await lstk(["--account", "111111111111", "terraform", "version"], {
      home,
      env: { PATH: terraform.path },
    });

    expect(run).toFail();
    expect(run.stdout).toPrintExactly(`
      Error: --region and --account must appear after the terraform subcommand (e.g. \`lstk terraform --region us-west-2 ...\`)
    `);
    expect(await terraform.calls()).toEqual([]);
  });
});

describe.skipIf(noDocker)("lstk terraform with a running emulator", () => {
  useExclusiveEmulator();

  afterEach(async () => {
    await docker.removeContainer(AWS_CONTAINER);
    await docker.removeContainer(SNOWFLAKE_CONTAINER);
  });

  test("fails with a clear message when no emulator is running", async () => {
    const terraform = await fakeTerraform();
    const home = await tempHome();

    const run = await lstk(["terraform", "plan"], { home, env: { PATH: terraform.path } });

    expect(run).toFail();
    expect(run.stdout).toPrintExactly(`
      Error: LocalStack AWS Emulator is not running
        ==> Start LocalStack: lstk
        ==> See help: lstk -h
    `);
    expect(await terraform.calls(), "terraform must never be invoked when nothing is running").toEqual([]);
  });

  test("requires the AWS emulator: fails clearly when Snowflake is running instead", async () => {
    await startPlaceholderEmulator(SNOWFLAKE_CONTAINER);
    const terraform = await fakeTerraform();
    const home = await tempHome();
    await home.writeConfig(`[[containers]]\ntype = "snowflake"\ntag = "latest"\nport = "4566"\n`);

    const run = await lstk(["terraform", "plan"], { home, env: { PATH: terraform.path } });

    expect(run).toFail();
    expect(run.stdout).toPrintExactly(`
      Error: lstk terraform requires the LocalStack AWS Emulator, but the LocalStack Snowflake Emulator is running
        ==> Start the AWS emulator: lstk
    `);
    expect(await terraform.calls()).toEqual([]);
  });

  test("a chdir target that does not exist fails before terraform is invoked", async () => {
    await startPlaceholderEmulator(AWS_CONTAINER);
    const terraform = await fakeTerraform();
    const home = await tempHome();

    const run = await lstk(["terraform", "-chdir=does-not-exist", "plan"], {
      home,
      env: { PATH: terraform.path },
    });

    expect(run).toFail();
    expect(run.stdout).toPrintExactly("Error: -chdir directory does not exist: does-not-exist");
    expect(await terraform.calls()).toEqual([]);
    expect(await fileExists(path.join(home.path, "does-not-exist", OVERRIDE_FILE))).toBe(false);
  });

  test("a provider schema that requires `terraform init` fails clearly and invokes terraform only once", async () => {
    await startPlaceholderEmulator(AWS_CONTAINER);
    const terraform = await fakeBinary({
      name: "terraform",
      responses: [
        { when: ["providers", "schema"], exitCode: 1, stderr: "Error: required providers not installed" },
      ],
    });
    const home = await tempHome();

    const run = await lstk(["terraform", "plan"], { home, env: { PATH: terraform.path } });

    expect(run).toFail();
    expect(run.stdout).toPrintExactly(`
      Error: Terraform AWS provider is not installed
        ==> Initialize the project: terraform init
    `);
    expect(planCall(await terraform.calls()), "the real subcommand must never run").toBeUndefined();
    expect(await fileExists(path.join(home.path, OVERRIDE_FILE))).toBe(false);
  });

  test("a pre-existing override file is refused, not overwritten", async () => {
    await startPlaceholderEmulator(AWS_CONTAINER);
    const terraform = await fakeTerraform();
    const home = await tempHome();
    const overridePath = path.join(home.path, OVERRIDE_FILE);
    await writeFile(overridePath, "# my own override\n");

    const run = await lstk(["terraform", "plan"], { home, env: { PATH: terraform.path } });

    expect(run).toFail();
    // This error is a plain Go error, not routed through the sink, so it falls
    // through to the top-level "Error: %v" fallback on stderr rather than
    // stdout like the other error events in this file. The path it names is
    // this test's own temp home, so it is masked rather than snapshotted raw.
    // macOS resolves the temp dir through its /private symlink, which the
    // built-in masking only strips from the *start* of the string, not from
    // this mid-string occurrence -- the extra pass normalizes that away so
    // the snapshot reads the same on macOS and Linux CI.
    expect(
      normalizeCliOutput(run.stderr, { home }),
    ).toPrintExactly("Error: refusing to overwrite existing file <home>/localstack_providers_override.tf — remove it or set LSTK_TF_OVERRIDE_FILE_NAME to a different name");
    expect(await readFile(overridePath, "utf8"), "the user's file must be untouched").toBe(
      "# my own override\n",
    );
  });

  test("LSTK_TF_DRY_RUN generates the override with resolved region/account and skips terraform", async () => {
    await startPlaceholderEmulator(AWS_CONTAINER);
    const terraform = await fakeTerraform();
    const home = await tempHome({ env: { LSTK_TF_DRY_RUN: "1" } });

    const run = await lstk(
      ["terraform", "--region", "us-west-2", "--account", "111111111111", "plan"],
      { home, env: { PATH: terraform.path } },
    );

    expect(run).toSucceed();
    expect(planCall(await terraform.calls()), "a dry run must not invoke the real subcommand").toBeUndefined();

    const content = await readFile(path.join(home.path, OVERRIDE_FILE), "utf8");
    expect(content).toContain('region = "us-west-2"');
    expect(content).toContain('access_key = "111111111111"');
    expect(content).toContain("endpoints {");
    expect(content).toContain("s3 =");
  });

  test("a proxied plan generates the override and removes it once terraform exits", async () => {
    await startPlaceholderEmulator(AWS_CONTAINER);
    const terraform = await fakeTerraform({ captureOverride: true });
    const home = await tempHome();

    const run = await lstk(["terraform", "plan"], { home, env: { PATH: terraform.path } });

    expect(run).toSucceed();
    const call = planCall(await terraform.calls());
    expect(call?.args).toEqual(["plan"]);
    expect(call?.files[OVERRIDE_FILE]).toContain("s3 =");
    expect(await fileExists(path.join(home.path, OVERRIDE_FILE))).toBe(false);
  });

  describe("-chdir anchors the override to the target directory", () => {
    test("a dry run writes the override inside the chdir dir, not the process cwd", async () => {
      await startPlaceholderEmulator(AWS_CONTAINER);
      const terraform = await fakeTerraform();
      const home = await tempHome({ env: { LSTK_TF_DRY_RUN: "1" } });
      await mkdir(path.join(home.path, "infra"));

      const run = await lstk(["terraform", "-chdir=infra", "plan"], { home, env: { PATH: terraform.path } });

      expect(run).toSucceed();
      const content = await readFile(path.join(home.path, "infra", OVERRIDE_FILE), "utf8");
      expect(content).toContain("s3 =");
      expect(await fileExists(path.join(home.path, OVERRIDE_FILE))).toBe(false);
    });

    test("a live run forwards -chdir to terraform and cleans up the override afterwards", async () => {
      await startPlaceholderEmulator(AWS_CONTAINER);
      const terraform = await fakeTerraform({ captureOverride: true });
      const home = await tempHome();
      await mkdir(path.join(home.path, "infra"));

      const run = await lstk(["terraform", "-chdir=infra", "plan"], { home, env: { PATH: terraform.path } });

      expect(run).toSucceed();
      const call = planCall(await terraform.calls());
      expect(call?.args).toEqual(["-chdir=infra", "plan"]);
      expect(await fileExists(path.join(home.path, "infra", OVERRIDE_FILE))).toBe(false);
      expect(await fileExists(path.join(home.path, OVERRIDE_FILE))).toBe(false);
    });
  });
});
