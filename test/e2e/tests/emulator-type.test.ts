import { describe, expect, test } from "vitest";
import { lstk, tempHome, unreachableDockerHost } from "../support/index.ts";

// Ported from test/integration/emulator_type_test.go.
//
// `--type` is defined as "rewrite the type line in config", not a per-run
// override. These tests assert the config mutation and the messaging; the start
// itself is expected to fail at the runtime ping, which is what keeps them fast
// and daemon-free.

const noDaemon = { env: { DOCKER_HOST: unreachableDockerHost, LOCALSTACK_AUTH_TOKEN: "dummy-token" } };

describe("lstk start --type", () => {
  test("creates the config on a first run", async () => {
    const home = await tempHome(noDaemon);
    expect(await home.configExists(), "this must look like a fresh install").toBe(false);

    const run = await lstk(["start", "--type", "snowflake", "--non-interactive"], { home });

    // Not snapshotted: every run here also hits the Docker-is-not-available
    // message (the runtime ping fails against `noDaemon`), whose suggested
    // start commands vary per machine -- same reason aws-proxy.test.ts gives
    // for not snapshotting it.
    expect(run).toPrint("Snowflake emulator selected.");
    expect(await home.readConfig()).toContain(`type = "snowflake"`);
  });

  test("switches an existing config in place, preserving comments and other fields", async () => {
    const home = await tempHome(noDaemon);
    await home.writeConfig(
      [
        "[[containers]]",
        `type = "aws"     # keep me`,
        `tag = "latest"`,
        `port = "4566"`,
        "",
      ].join("\n"),
    );

    const run = await lstk(["start", "--type", "azure", "--non-interactive"], { home });

    // Not snapshotted: same reason as the test above -- the Docker-unavailable
    // message that follows varies per machine.
    expect(run).toPrint("Switched configured emulator to Azure");
    const config = await home.readConfig();
    expect(config).toContain(`type = "azure"`);
    expect(config, "the rewrite is surgical").toContain("# keep me");
    expect(config).toContain(`port = "4566"`);
  });

  test("leaves the config untouched when it already matches", async () => {
    const home = await tempHome(noDaemon);
    const original = `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\n`;
    await home.writeConfig(original);

    const run = await lstk(["start", "--type", "aws", "--non-interactive"], { home });

    // Not snapshotted: the whole output here is the Docker-unavailable
    // message, which varies per machine; this absence check is what's stable.
    expect(run).not.toPrint("Switched configured emulator");
    expect(await home.readConfig()).toBe(original);
  });
});
