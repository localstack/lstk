import { describe, expect, test } from "vitest";
import {
  dockerIsAvailable,
  lstkPty,
  mockLicenseServer,
  requirement,
  tempHome,
  useExclusiveEmulator,
} from "../support/index.ts";

// Ported from test/integration/emulator_select_test.go.
//
// The first-run emulator picker only exists on a terminal, so these run lstk on
// a PTY. The picker must appear exactly when there is no config yet, and the
// choice must be persisted before the confirmation is printed.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);

describe("first-run emulator selection", () => {
  test("does not prompt when a config already exists", async () => {
    const home = await tempHome();
    await home.writeConfig(`[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\n`);

    const term = lstkPty(["start"], { home });

    await term.expectNever("Which emulator would you like to use?", { within: 2_000 });
  });

  // The picker only appears when no config file exists yet, which is also
  // exactly the state that gives the started emulator the canonical
  // `localstack-aws` name (no tag to privatize with) -- so this case keeps
  // the machine-wide lock rather than a private emulator identity.
  describe.skipIf(noDocker)("on a fresh install", () => {
    useExclusiveEmulator();

    test("prompts, and persists the choice before confirming it", async () => {
      // A token (any token, validated against the mock) keeps the run from
      // stopping at the interactive login before it reaches the picker.
      const license = await mockLicenseServer("grants");
      const home = await tempHome({
        env: { LSTK_API_ENDPOINT: license.url, LOCALSTACK_AUTH_TOKEN: "fake-token" },
      });
      expect(await home.configExists()).toBe(false);

      const term = lstkPty(["start"], { home });

      await term.waitFor("Which emulator would you like to use?");
      term.press("enter"); // confirm the default-highlighted option (AWS)
      await term.waitFor("AWS emulator selected.");

      expect(await home.readConfig()).toContain(`type = "aws"`);
    });
  });
});
