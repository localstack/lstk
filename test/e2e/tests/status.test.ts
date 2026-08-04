import http from "node:http";
import { onTestFinished } from "vitest";
import { describe, expect, test } from "vitest";
import { CreateBucketCommand, S3Client } from "@aws-sdk/client-s3";
import { CreateQueueCommand, SQSClient } from "@aws-sdk/client-sqs";
import {
  authToken,
  docker,
  dockerCanBindLoopbackAlias,
  dockerIsAvailable,
  lstk,
  requireAuthToken,
  requirement,
  tempHome,
  useExclusiveEmulator,
} from "../support/index.ts";
import { privateEmulator, startStubEmulator } from "../support/emulator-stub.ts";

// Ported from test/integration/status_test.go.
//
// `lstk status` makes its own HTTP calls to the emulator (health + resources),
// separately from how it discovers whether a container is running. Most of
// these tests exploit that split the same way the Go suite does: a plain
// `sleep infinity` container under the name lstk expects (`localstack-aws` /
// `localstack-snowflake`) satisfies the "is it running" check, while a small
// local HTTP server stands in for the emulator's `/_localstack/health` and
// `/_localstack/resources` endpoints, reached via the `LOCALSTACK_HOST` env
// var lstk itself honors as a host override. This exercises the real output
// parsing/rendering without pulling a multi-hundred-MB licensed image.
//
// Most of these only need "an emulator is running" under some name, so they
// give themselves a private container name/tag via `privateEmulator()`
// instead of the canonical `localstack-<type>` name, and need no lock. A
// small subset genuinely depends on the canonical name or a real published
// port; those stay under `useExclusiveEmulator()` below, with a comment on
// why each one can't be privatized.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);
const noLoopbackAlias =
  noDocker ||
  requirement(
    "publishing a container port on a loopback alias",
    await dockerCanBindLoopbackAlias(),
    "Use a native Linux daemon: Docker Desktop's VM networking cannot bind 127.0.0.2.",
  );

/** A stand-in for the emulator's health/resources endpoints. */
function mockLocalStackServer(opts: {
  version: string;
  resourcesBody?: string;
}): Promise<{ hostPort: string }> {
  const server = http.createServer((req, res) => {
    if (req.url === "/_localstack/health") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ version: opts.version, services: {} }));
      return;
    }
    if (req.url === "/_localstack/resources") {
      res.writeHead(200, { "Content-Type": "application/x-ndjson" });
      res.end(opts.resourcesBody ?? "");
      return;
    }
    res.writeHead(404).end();
  });

  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      onTestFinished(() => new Promise<void>((done) => server.close(() => done())));
      const address = server.address();
      if (address === null || typeof address === "string") {
        throw new Error("mock LocalStack server did not bind to a TCP port");
      }
      resolve({ hostPort: `127.0.0.1:${address.port}` });
    });
  });
}

describe.skipIf(noDocker)("lstk status", () => {
  test("shows no resources when the emulator reports an empty environment", async () => {
    const mock = await mockLocalStackServer({ version: "4.14.1" });
    const emu = privateEmulator();
    await startStubEmulator(emu.name);

    const home = await tempHome({ env: { LOCALSTACK_HOST: mock.hostPort } });
    await home.writeConfig(emu.config);

    const run = await lstk(["status"], { home });

    // Not snapshotted: same reason as the test above -- Endpoint/Uptime vary.
    expect(run).toSucceed();
    expect(run).toPrint("No resources deployed");
  });

  test("reports no resource table for a running Snowflake emulator", async () => {
    const emu = privateEmulator("snowflake");
    await startStubEmulator(emu.name);

    const home = await tempHome();
    await home.writeConfig(emu.config);

    const run = await lstk(["status"], { home });

    // Not snapshotted: the Uptime line varies (0s vs 1s+ depending on machine
    // speed), so the full output is not reproducible run to run.
    expect(run).toSucceed();
    expect(run).toPrint("Snowflake");
    expect(run).toPrint("running");
    expect(
      run,
      "snowflake status should display the snowflake-routed host clients use to connect",
    ).toPrint("snowflake.localhost.localstack.cloud:4566");
    // Snowflake does not expose AWS resources: no resource table, no empty-state note.
    expect(run).not.toPrint("SERVICE");
    expect(run).not.toPrint("No resources deployed");
  });

  // These cannot use a private emulator identity, so they keep the
  // machine-wide lock instead:
  describe("against the canonical name or a real published port", () => {
    useExclusiveEmulator();

    test("fails with a not-running message and a help pointer when nothing is running", async () => {
      // No config is written at all, so this exercises the true zero-config
      // default (AWS, canonical name `localstack-aws`, port 4566). A private
      // tag would sidestep that default entirely, and a real `localstack-aws`
      // started concurrently elsewhere would make "not running" flaky.
      const home = await tempHome();

      const run = await lstk(["status"], { home });

      expect(run).toExitWith(1);
      expect(run.stdout).toPrintExactly(`
        Error: LocalStack AWS Emulator is not running
          ==> Start LocalStack: lstk
          ==> See help: lstk -h
      `);
    });

    // "status uses the port the container is actually bound to, not the one the
    // config still claims". Publishing on the 127.0.0.2 loopback alias is what
    // lets a mock server hold the same port number on 127.0.0.1, and Docker
    // Desktop's VM-backed networking rejects that ("bind: can't assign requested
    // address") where a native Linux daemon accepts it — so this is gated rather
    // than dropped or left flaky.
    test.skipIf(noLoopbackAlias)(
      "reports the port the container is bound to, not the stale one in config",
      async () => {
        const mock = await mockLocalStackServer({ version: "4.14.1" });
        const mockPort = mock.hostPort.split(":")[1]!;

        await startStubEmulator("localstack-aws", {
          hostBinding: { hostPort: mockPort, hostIp: "127.0.0.2" },
        });

        // The config still says 4566 — as it would for a user who edited it
        // after starting the container.
        const home = await tempHome();
        await home.writeConfig(`[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\n`);

        const run = await lstk(["status"], { home });

        expect(run).toSucceed();
        expect(
          run,
          "the version can only come from the mock, which is only reachable on the bound port",
        ).toPrint("4.14.1");
      },
    );

    test("works with a container started outside lstk", async () => {
      // Deliberately exercises the image/port discovery fallback: a foreign
      // container tagged as a real `localstack/*` image repo and published on
      // the canonical port 4566, which only lstk's fallback discovery -- not a
      // container name -- distinguishes from another emulator on that port.
      const mock = await mockLocalStackServer({ version: "3.5.0" });

      const fakeImage = "localstack/localstack-pro:test-fake";
      await docker.pull("alpine:latest");
      await docker.tag("alpine:latest", fakeImage);
      await startStubEmulator("localstack-external", {
        image: fakeImage,
        hostBinding: { hostPort: "4566" },
      });

      const home = await tempHome({ env: { LOCALSTACK_HOST: mock.hostPort } });

      const run = await lstk(["status"], { home });

      // Not snapshotted: the full status output also carries the mock server's
      // ephemeral port (Endpoint) and an Uptime that ticks between runs, per the
      // README's note on `lstk status` output. The version is the one stable,
      // load-bearing fact here.
      expect(run).toSucceed();
      expect(run).toPrint("3.5.0");
    });

    // The one test that reports resources a real emulator really holds, rather
    // than ones a mock claimed: it starts a licensed emulator, creates an S3
    // bucket and an SQS queue through the AWS SDK, and reads them back out of
    // `lstk status`. Everything else here mocks /_localstack/resources, which
    // exercises the rendering but not that the two ends agree.
    test.skipIf(!authToken())("lists resources a real emulator actually holds", async () => {
      const home = await tempHome({ env: { LOCALSTACK_AUTH_TOKEN: requireAuthToken() } });
      await home.writeConfig(`[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\n`);

      // The block-level cleanup only runs after the last test in it, so a real
      // emulator left up here would still be holding port 4566 under the
      // canonical name when the next test starts one.
      onTestFinished(async () => {
        await docker.removeContainer("localstack-aws");
      });

      const start = await lstk(["start", "--non-interactive"], { home });
      expect(start).toSucceed();

      const clientConfig = {
        region: "us-east-1",
        endpoint: "http://localhost:4566",
        credentials: { accessKeyId: "test", secretAccessKey: "test" },
      };
      await new S3Client({ ...clientConfig, forcePathStyle: true }).send(
        new CreateBucketCommand({ Bucket: "my-test-bucket" }),
      );
      await new SQSClient(clientConfig).send(new CreateQueueCommand({ QueueName: "my-test-queue" }));

      const run = await lstk(["status"], { home });

      expect(run).toSucceed();
      // Not snapshotted: Uptime ticks, and the emulator's own version varies
      // with whatever `latest` is today.
      expect(run).toPrint("running");
      expect(run).toPrint(/SERVICE\s+RESOURCE/);
      expect(run).toPrint(/S3\s+my-test-bucket/);
      // SQS names a queue by its URL, not the bare name it was created with.
      expect(run).toPrint(/SQS\s+\S*\/my-test-queue\b/);
    });

    test.skipIf(!authToken())(
      "shows the version reported by a running Snowflake emulator",
      async () => {
        // Starts a real, license-validated emulator via `lstk start`, which
        // always uses the canonical name -- not a stub under a private tag --
        // and requires LOCALSTACK_AUTH_TOKEN (absent here, so this skips).
        const home = await tempHome({
          env: { LOCALSTACK_AUTH_TOKEN: requireAuthToken() },
        });
        await home.writeConfig(`[[containers]]\ntype = "snowflake"\ntag = "latest"\nport = "4566"\n`);

        const start = await lstk(["start", "--non-interactive"], { home });
        expect(start).toSucceed();

        const health = (await fetch("http://localhost:4566/_localstack/health").then((r) =>
          r.json(),
        )) as { version: string };
        expect(health.version).toBeTruthy();

        const run = await lstk(["status"], { home });

        expect(run).toSucceed();
        expect(
          run,
          "snowflake status should display the version reported by /_localstack/health",
        ).toPrint(`• Version: ${health.version}`);
      },
    );
  });
});
