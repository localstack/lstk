import { describe, expect, test } from "vitest";
import {
  authToken,
  dockerIsAvailable,
  lstk,
  mockLicenseServer,
  requireAuthToken,
  requirement,
  tempHome,
  useExclusiveEmulator,
} from "../support/index.ts";

// Ported from test/integration/start_test.go.
//
// The flagship happy path: a real emulator container comes up and lstk reports
// success. Needs Docker and a real auth token; skips without either.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

describe.skipIf(noDocker || !authToken())("lstk start", () => {
  useExclusiveEmulator();

  test("starts the AWS emulator with a valid token", async () => {
    const license = await mockLicenseServer("grants");
    const home = await tempHome({
      env: {
        LSTK_API_ENDPOINT: license.url,
        LOCALSTACK_AUTH_TOKEN: requireAuthToken(),
      },
    });

    const run = await lstk(["start", "--non-interactive"], { home });

    expect(run).toSucceed();
    expect(
      run,
      "the persistence bullet must be omitted when --persist is not set",
    ).not.toPrint("• Persistence:");

    // What "started" means to a user: the CLI now reports a running emulator and
    // an endpoint to talk to. Asserted through lstk itself rather than by looking
    // for a container of a particular name.
    const status = await lstk(["status"], { home });
    expect(status).toSucceed();
    expect(status).toPrint("is running");
    expect(status).toPrint("• Endpoint:");
  });
});
