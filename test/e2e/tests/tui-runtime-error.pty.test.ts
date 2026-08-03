import { describe, expect, test } from "vitest";
import { lstkPty, tempHome, unreachableDockerHost } from "../support/index.ts";

// The one TUI test with no Docker and no browser in it, so it runs on every
// platform — including Windows, where the Go suite skips all PTY tests
// (creack/pty has no Windows support). If Bubble Tea behaves over ConPTY, this is
// what proves it; if it does not, this is where it shows up first.

describe("the interactive start path", () => {
  test("renders an unreachable container runtime as an error and exits non-zero", async () => {
    const home = await tempHome({
      env: { DOCKER_HOST: unreachableDockerHost, LOCALSTACK_AUTH_TOKEN: "dummy-token" },
    });
    // A config means this is not a first run, so nothing waits for input.
    await home.writeConfig(`[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\n`);

    const term = lstkPty(["start"], { home });

    await term.waitFor(/Docker is not available|cannot connect to Docker daemon/);
    expect(await term.exitCode(), "a failed start must not report success").not.toBe(0);
  });
});
