import { describe, expect, test } from "vitest";
import {
  dockerIsAvailable,
  lstkPty,
  mockLicenseServer,
  requirement,
  tempHome,
} from "../support/index.ts";

// Ported from test/integration/non_interactive_test.go.
//
// These run on a PTY on purpose: without a real terminal attached, stdin/stdout
// already aren't a TTY, so a plain (non-PTY) invocation would take the
// non-interactive path anyway and never prove that `--non-interactive` itself
// is what forces it.

describe("--non-interactive blocks login", () => {
  test("login --non-interactive fails instead of waiting for a browser", async () => {
    const home = await tempHome();

    const term = lstkPty(["login", "--non-interactive"], { home });

    expect(await term.exitCode()).toBe(1);
    expect(term.output()).toContain("login requires an interactive terminal");
  });
});

// Reaching the "authentication required" message needs a live Docker daemon:
// container.Start pings the runtime before it ever checks for a token (see
// internal/container/start.go), so without Docker the run fails earlier with a
// runtime error instead of the message these tests assert on.
const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

describe.skipIf(noDocker)("--non-interactive fails fast without a token", () => {
  test("lstk start --non-interactive", async () => {
    const license = await mockLicenseServer("grants");
    const home = await tempHome({ env: { LSTK_API_ENDPOINT: license.url } });

    const term = lstkPty(["start", "--non-interactive"], { home });

    expect(await term.exitCode()).toBe(1);
    expect(term.output()).toContain(
      "authentication required: set LOCALSTACK_AUTH_TOKEN or run in interactive mode",
    );
  });

  test("bare lstk --non-interactive", async () => {
    const license = await mockLicenseServer("grants");
    const home = await tempHome({ env: { LSTK_API_ENDPOINT: license.url } });

    const term = lstkPty(["--non-interactive"], { home });

    expect(await term.exitCode()).toBe(1);
    expect(term.output()).toContain(
      "authentication required: set LOCALSTACK_AUTH_TOKEN or run in interactive mode",
    );
  });
});
