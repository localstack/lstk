import http from "node:http";
import { onTestFinished } from "vitest";
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
import { startStubEmulator } from "../support/emulator-stub.ts";

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

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
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
  useExclusiveEmulator();

  test("fails with a not-running message and a help pointer when nothing is running", async () => {
    const home = await tempHome();

    const run = await lstk(["status"], { home });

    expect(run).toExitWith(1);
    expect(run.stdout).toPrintExactly(`
      Error: LocalStack AWS Emulator is not running
        ==> Start LocalStack: lstk
        ==> See help: lstk -h
    `);
  });

  // NOTE: test/integration/status_test.go also covers "status uses the actual
  // bound port rather than a stale configured one" by publishing a placeholder
  // container's port on a second loopback alias (127.0.0.2) so a mock server
  // can occupy the same port number on 127.0.0.1. That relies on Docker being
  // able to publish to an arbitrary loopback address, which Docker Desktop's
  // VM-backed networking on this machine rejects ("bind: can't assign
  // requested address"), unlike the native Linux daemon the Go suite runs
  // against in CI. Not reproducible deterministically across contributor
  // machines here, so it is dropped rather than left flaky.

  test("works with a container started outside lstk", async () => {
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

  test("shows no resources when the emulator reports an empty environment", async () => {
    const mock = await mockLocalStackServer({ version: "4.14.1" });
    await startStubEmulator("localstack-aws");

    const home = await tempHome({ env: { LOCALSTACK_HOST: mock.hostPort } });

    const run = await lstk(["status"], { home });

    // Not snapshotted: same reason as the test above -- Endpoint/Uptime vary.
    expect(run).toSucceed();
    expect(run).toPrint("No resources deployed");
  });

  test("reports no resource table for a running Snowflake emulator", async () => {
    await startStubEmulator("localstack-snowflake");

    const home = await tempHome();
    await home.writeConfig(`[[containers]]\ntype = "snowflake"\ntag = "latest"\nport = "4566"\n`);

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

  test.skipIf(!authToken())(
    "shows the version reported by a running Snowflake emulator",
    async () => {
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
