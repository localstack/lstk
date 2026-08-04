import { describe, expect, test } from "vitest";
import {
  docker,
  dockerIsAvailable,
  lstk,
  requirement,
  tempHome,
  unreachableDockerHost,
} from "../support/index.ts";
import { fakeBinary } from "../support/fake-binary.ts";
import { emulatorApi, notLocalStackServer } from "../support/emulator-api.ts";
import { privateEmulator, startStubEmulator } from "../support/emulator-stub.ts";

// Ported from test/integration/endpoint_url_test.go.
//
// `--endpoint-url` (and its LSTK_ENDPOINT_URL / AWS_ENDPOINT_URL equivalents)
// points lstk at an emulator it did not start. Every test here pins DOCKER_HOST
// to a socket that cannot exist, so "Docker was never consulted" is proven by the
// command succeeding at all rather than by inspecting anything.
//
// Dropped: the telemetry assertions. Which analytics event fired is not something
// a user observes — see test/integration/command_telemetry_test.go.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

/** Environment for a command that must never reach a container runtime. */
const noDaemon = { env: { DOCKER_HOST: unreachableDockerHost } };

const url = "http://localhost:4566";

// logs/stop/restart/start/volume act on a local container or on local filesystem
// state, so there is nothing for an endpoint to mean. Rejecting is the whole
// behaviour, and each is rejected identically however the endpoint arrived:
// lstk's own `--endpoint-url` flag, or the ambient LSTK_ENDPOINT_URL /
// AWS_ENDPOINT_URL. An ambient value is refused just as loudly as an explicit
// flag — silently proceeding against a local container was the reported bug.
//
// Spelled out per row rather than assembled from a command list and a source
// list: every row shows the exact argv and environment it runs with, which is
// what a reader needs, and the differences between rows (`volume clear` wants
// --force, `start` wants --non-interactive) stay visible instead of hiding in a
// helper.
interface Rejection {
  /** Command name as lstk names it back in the error. */
  command: string;
  /** How the endpoint reached lstk, spelled as the error spells it. */
  source: string;
  /** The parenthetical the error ends with. */
  suffix: string;
  args: string[];
  env: Record<string, string>;
}

const rejections: Rejection[] = [
  { command: "logs", source: "--endpoint-url", suffix: "--endpoint-url was passed",
    args: ["logs", "--endpoint-url", url], env: {} },
  { command: "logs", source: "LSTK_ENDPOINT_URL", suffix: "LSTK_ENDPOINT_URL is set",
    args: ["logs"], env: { LSTK_ENDPOINT_URL: url } },
  { command: "logs", source: "AWS_ENDPOINT_URL", suffix: "AWS_ENDPOINT_URL is set",
    args: ["logs"], env: { AWS_ENDPOINT_URL: url } },

  { command: "stop", source: "--endpoint-url", suffix: "--endpoint-url was passed",
    args: ["stop", "--endpoint-url", url], env: {} },
  { command: "stop", source: "LSTK_ENDPOINT_URL", suffix: "LSTK_ENDPOINT_URL is set",
    args: ["stop"], env: { LSTK_ENDPOINT_URL: url } },
  { command: "stop", source: "AWS_ENDPOINT_URL", suffix: "AWS_ENDPOINT_URL is set",
    args: ["stop"], env: { AWS_ENDPOINT_URL: url } },

  { command: "restart", source: "--endpoint-url", suffix: "--endpoint-url was passed",
    args: ["restart", "--endpoint-url", url], env: {} },
  { command: "restart", source: "LSTK_ENDPOINT_URL", suffix: "LSTK_ENDPOINT_URL is set",
    args: ["restart"], env: { LSTK_ENDPOINT_URL: url } },
  { command: "restart", source: "AWS_ENDPOINT_URL", suffix: "AWS_ENDPOINT_URL is set",
    args: ["restart"], env: { AWS_ENDPOINT_URL: url } },

  { command: "start", source: "--endpoint-url", suffix: "--endpoint-url was passed",
    args: ["start", "--non-interactive", "--endpoint-url", url], env: {} },
  { command: "start", source: "LSTK_ENDPOINT_URL", suffix: "LSTK_ENDPOINT_URL is set",
    args: ["start", "--non-interactive"], env: { LSTK_ENDPOINT_URL: url } },
  { command: "start", source: "AWS_ENDPOINT_URL", suffix: "AWS_ENDPOINT_URL is set",
    args: ["start", "--non-interactive"], env: { AWS_ENDPOINT_URL: url } },

  { command: "volume path", source: "--endpoint-url", suffix: "--endpoint-url was passed",
    args: ["volume", "path", "--endpoint-url", url], env: {} },
  { command: "volume path", source: "LSTK_ENDPOINT_URL", suffix: "LSTK_ENDPOINT_URL is set",
    args: ["volume", "path"], env: { LSTK_ENDPOINT_URL: url } },
  { command: "volume path", source: "AWS_ENDPOINT_URL", suffix: "AWS_ENDPOINT_URL is set",
    args: ["volume", "path"], env: { AWS_ENDPOINT_URL: url } },

  { command: "volume clear", source: "--endpoint-url", suffix: "--endpoint-url was passed",
    args: ["volume", "clear", "--force", "--endpoint-url", url], env: {} },
  { command: "volume clear", source: "LSTK_ENDPOINT_URL", suffix: "LSTK_ENDPOINT_URL is set",
    args: ["volume", "clear", "--force"], env: { LSTK_ENDPOINT_URL: url } },
  { command: "volume clear", source: "AWS_ENDPOINT_URL", suffix: "AWS_ENDPOINT_URL is set",
    args: ["volume", "clear", "--force"], env: { AWS_ENDPOINT_URL: url } },
];

describe("commands with no remote equivalent", () => {
  test.each(rejections)("$command rejects $source", async ({ command, source, suffix, args, env }) => {
    const home = await tempHome();

    const run = await lstk(args, { home, env: { ...noDaemon.env, ...env } });

    expect(run).toExitWith(1);
    expect(run.stdout).toPrintExactly(
      `Error: ${command} does not support ${source}: it operates on a local Docker container or local filesystem state with no remote equivalent (${suffix})`,
    );
    expect(run.stderr, "the rejection is rendered once, through the sink").toPrintExactly("");
  });
});

// The rejection must not depend on whether a local emulator happens to be
// running: with one genuinely up and reachable, `stop` used to proceed and stop
// this exact container instead of refusing.
describe.skipIf(noDocker)("with a local emulator genuinely running", () => {
  test("stop still rejects an ambient LSTK_ENDPOINT_URL, and leaves the container untouched", async () => {
    const emulator = privateEmulator();
    await startStubEmulator(emulator.name);
    const home = await tempHome({ env: { LSTK_ENDPOINT_URL: "http://127.0.0.1:1" } });
    await home.writeConfig(emulator.config);

    const run = await lstk(["stop"], { home });

    expect(run).toExitWith(1);
    expect(run.stdout).toPrintExactly(
      "Error: stop does not support LSTK_ENDPOINT_URL: it operates on a local Docker container or local filesystem state with no remote equivalent (LSTK_ENDPOINT_URL is set)",
    );
    expect(
      await docker.containerIsRunning(emulator.name),
      "stop must reject before ever touching the container",
    ).toBe(true);
  });

  test("restart still rejects an ambient LSTK_ENDPOINT_URL, and leaves the container untouched", async () => {
    const emulator = privateEmulator();
    await startStubEmulator(emulator.name);
    const home = await tempHome({ env: { LSTK_ENDPOINT_URL: "http://127.0.0.1:1" } });
    await home.writeConfig(emulator.config);

    const run = await lstk(["restart"], { home });

    expect(run).toExitWith(1);
    expect(run.stdout).toPrintExactly(
      "Error: restart does not support LSTK_ENDPOINT_URL: it operates on a local Docker container or local filesystem state with no remote equivalent (LSTK_ENDPOINT_URL is set)",
    );
    expect(
      await docker.containerIsRunning(emulator.name),
      "restart must reject before ever touching the container",
    ).toBe(true);
  });
});

describe("lstk aws", () => {
  test("takes the endpoint without ever reaching Docker", async () => {
    const api = await emulatorApi({ version: "3.0.2" });
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", api.url, "aws", "s3", "ls"], {
      home,
      env: { ...noDaemon.env, PATH: aws.path },
    });

    expect(run).toSucceed();
    expect((await aws.lastCall())?.args).toEqual(["--endpoint-url", api.url, "s3", "ls"]);
  });

  // The AWS CLI has its own --endpoint-url. lstk claims only the occurrence
  // before the subcommand; one after it belongs to the wrapped tool and must
  // arrive verbatim. The pre-command value is a different server, so lstk's own
  // Docker bypass is proven by the command working at all.
  test("leaves an --endpoint-url after the subcommand for the aws CLI itself", async () => {
    const api = await emulatorApi({ version: "3.0.2" });
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();

    const run = await lstk(
      ["--endpoint-url", api.url, "aws", "--endpoint-url", "http://127.0.0.1:9", "s3", "ls"],
      { home, env: { ...noDaemon.env, PATH: aws.path } },
    );

    expect(run).toSucceed();
    expect((await aws.lastCall())?.args).toEqual([
      "--endpoint-url",
      api.url,
      "--endpoint-url",
      "http://127.0.0.1:9",
      "s3",
      "ls",
    ]);
  });
});

describe("lstk status", () => {
  test("renders reachability, type and version, with no container fields", async () => {
    const api = await emulatorApi({ version: "3.0.2", services: { s3: "available" } });
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", api.url, "status"], { home, ...noDaemon });

    expect(run).toSucceed();
    // Fully determined apart from the ephemeral port: an externally-managed
    // endpoint has no Container or Uptime line, which is the point.
    expect(run.stdout).toPrintExactly(`
      Fetching LocalStack status...
      ✔︎ LocalStack AWS Emulator is running
      • Endpoint: ${api.url}
      • Version: 3.0.2
      > Note: No resources deployed
    `);
  });

  // Deployed resources are an ordinary emulator API call, not something derived
  // from Docker, so an externally-managed target reports them exactly as a
  // Docker-managed one does. The first cut of --endpoint-url omitted them.
  test("reports deployed resources for an externally-managed endpoint", async () => {
    const api = await emulatorApi({
      version: "3.0.2",
      resources: `{"AWS::S3::Bucket": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "my-test-bucket"}]}\n`,
    });
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", api.url, "status"], { home, ...noDaemon });

    expect(run).toSucceed();
    expect(run.stdout).toPrintExactly(`
      Fetching LocalStack status...
      ✔︎ LocalStack AWS Emulator is running
      • Endpoint: ${api.url}
      • Version: 3.0.2
      ~ 1 resources · 1 services
        SERVICE  RESOURCE        REGION     ACCOUNT
        S3       my-test-bucket  us-east-1  000000000000
    `);
  });

  // An endpoint that answers but is not LocalStack must fail closed rather than
  // be reported as a running emulator. It also must not suggest `lstk start`:
  // the user is pointing at something they manage themselves.
  test("fails closed when the endpoint is not a LocalStack emulator", async () => {
    const server = await notLocalStackServer();
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", server.url, "status"], { home, ...noDaemon });

    expect(run).toExitWith(1);
    expect(run.stderr).toPrintExactly(
      `Error: could not reach LocalStack emulator at ${server.url}: unexpected status 404 from ${server.url}/_localstack/health`,
    );
    expect(run).not.toPrint("lstk start");
  });

  // An `https://` URL aimed at a plain-HTTP emulator fails on the TLS handshake,
  // which says nothing about the real problem. lstk re-probes the other scheme
  // and names the URL to retry. The https-first mirror of this needs certificate
  // trust and lives in endpoint-url-https.test.ts.
  test("suggests the http URL when https was used against a plain-HTTP emulator", async () => {
    const api = await emulatorApi({ version: "3.0.2" });
    const httpsUrl = api.url.replace("http://", "https://");
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", httpsUrl, "status"], { home, ...noDaemon });

    expect(run).toExitWith(1);
    expect(run).toPrint(`could not reach LocalStack emulator at ${httpsUrl}`);
    expect(run).toPrint(`${api.url} responded`);
    expect(run).toPrint("retry with that URL");
  });
});

describe("lstk cdk", () => {
  const cdk = { name: "cdk", responses: [{ when: ["--version"], stdout: "2.177.0\n" }] };

  // The breaking change: AWS_ENDPOINT_URL alone now skips Docker discovery for an
  // AWS-contacting cdk subcommand. It used to relabel the value while discovery
  // still ran, so the command failed without a daemon.
  test("AWS_ENDPOINT_URL alone bypasses the Docker check", async () => {
    const api = await emulatorApi({ version: "3.0.2" });
    const fake = await fakeBinary(cdk);
    const home = await tempHome();

    const run = await lstk(["cdk", "deploy", "MyStack"], {
      home,
      env: { ...noDaemon.env, PATH: fake.path, AWS_ENDPOINT_URL: api.url },
    });

    expect(run).toSucceed();
    const call = await fake.lastCall();
    expect(call?.args).toEqual(["deploy", "MyStack"]);
    expect(call?.env.AWS_ENDPOINT_URL).toBe(api.url);
  });

  // Emulator type is detected by probing, never configured — so an AWS-only proxy
  // pointed at an Azure endpoint is refused with the same shape used for a wrong
  // locally-running emulator.
  test("refuses an endpoint detected as a non-AWS emulator", async () => {
    // No `version` in /_localstack/health plus a populated /_localstack/info is
    // how lstk recognises Azure.
    const api = await emulatorApi({ services: {}, info: { version: "3.0.2" } });
    const fake = await fakeBinary(cdk);
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", api.url, "cdk", "deploy", "MyStack"], {
      home,
      env: { ...noDaemon.env, PATH: fake.path },
    });

    expect(run).toExitWith(1);
    expect(run).toPrint("requires the AWS emulator");
    expect(run).toPrint("Azure");
    expect(await fake.lastCall(), "cdk must never be invoked for a non-AWS endpoint").toBeUndefined();
  });
});

// `snapshot show` and a bare `snapshot list` only ever call the LocalStack
// platform API, which is account-scoped rather than emulator-scoped. They ignore
// --endpoint-url instead of rejecting it: the flag is a root persistent flag, and
// refusing it would make `lstk --endpoint-url ... snapshot list` fail for no
// reason. Proven by the command failing on its own platform call, not on flag
// validation.
describe("snapshot commands that never touch the emulator", () => {
  test("snapshot show ignores --endpoint-url rather than rejecting it", async () => {
    const home = await tempHome();

    const run = await lstk(
      ["snapshot", "show", "pod:my-baseline", "--endpoint-url", "http://localhost:4566"],
      { home, env: { ...noDaemon.env, LSTK_API_ENDPOINT: "http://127.0.0.1:1" } },
    );

    expect(run).toFail();
    expect(run).not.toPrint("does not support --endpoint-url");
  });

  test("a bare snapshot list ignores --endpoint-url rather than rejecting it", async () => {
    const home = await tempHome();

    const run = await lstk(["snapshot", "list", "--endpoint-url", "http://localhost:4566"], {
      home,
      env: { ...noDaemon.env, LSTK_API_ENDPOINT: "http://127.0.0.1:1" },
    });

    expect(run).toFail();
    expect(run).not.toPrint("does not support --endpoint-url");
  });
});
