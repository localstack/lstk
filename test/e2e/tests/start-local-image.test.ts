import { describe, expect, test } from "vitest";
import {
  docker,
  dockerIsAvailable,
  lstk,
  mockLicenseServer,
  requirement,
  tempHome,
  useExclusiveEmulator,
} from "../support/index.ts";

// Ported from test/integration/start_test.go (PRO-323).
//
// A pinned image that is already present locally must be reused, not re-pulled —
// what a user notices is a start that neither waits on the network nor re-downloads
// gigabytes. Neither of those is observable from outside the process, so this is the
// one test in the suite that asserts an internal decision through the message lstk
// prints about it. Everything else asserts behaviour; see README "Assert behaviour,
// not mechanism".
//
// A lightweight stand-in image is tagged as a pinned localstack-pro tag: only the
// pull decision is asserted, since the stand-in is not a real emulator and the start
// fails right after.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

describe.skipIf(noDocker)("lstk start with a pinned image", () => {
  useExclusiveEmulator();

  test("reuses an image that is already present locally", async () => {
    const pinnedTag = "reuse-local-test";
    const pinnedImage = `localstack/localstack-pro:${pinnedTag}`;
    await docker.pull("alpine:latest");
    await docker.tag("alpine:latest", pinnedImage);

    const license = await mockLicenseServer("grants");
    const home = await tempHome({
      env: { LSTK_API_ENDPOINT: license.url, LOCALSTACK_AUTH_TOKEN: "fake-token" },
    });
    // A dedicated port keeps this off the 4566 the other container tests use.
    await home.writeConfig(
      `[[containers]]\ntype = "aws"\ntag = "${pinnedTag}"\nport = "4599"\n`,
    );

    const run = await lstk(["start", "--non-interactive"], { home });

    expect(run).toPrint(`Using local image ${pinnedImage}`);
    expect(run, "an image already present must not be re-pulled").not.toPrint("Pulling");

    await docker.removeContainer(`localstack-aws-${pinnedTag}`);
  });
});
