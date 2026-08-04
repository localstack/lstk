import { describe, expect, test } from "vitest";
import { lstkPty, tempHome, unreachableDockerHost } from "../support/index.ts";
import { emulatorApi } from "../support/emulator-api.ts";

// Ported from test/integration/endpoint_url_test.go's
// TestStatusEndpointURLInteractiveRendersTUI.
//
// Needs a real terminal: the whole point is that the externally-managed path
// renders through the same Bubble Tea TUI as the Docker-managed one. It used to
// hardcode the plain sink regardless of interactive mode, so `--endpoint-url
// status` came out unstyled and without the blank lines the TUI puts around the
// resource summary — visibly different output for the same command.

describe("lstk --endpoint-url status on a terminal", () => {
  test("renders through the TUI, not the plain sink", async () => {
    const api = await emulatorApi({
      version: "3.0.2",
      services: { s3: "available" },
      resources: `{"AWS::S3::Bucket": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "my-test-bucket"}]}\n`,
    });
    const home = await tempHome({ env: { DOCKER_HOST: unreachableDockerHost } });

    const term = lstkPty(["--endpoint-url", api.url, "status"], { home });
    expect(await term.exitCode()).toBe(0);

    const lines = term.output().split("\n").map((line) => line.trimEnd());
    const summary = lines.findIndex((line) => line.includes("resources ·"));
    expect(summary, `resource summary should be rendered, got:\n${term.output()}`).toBeGreaterThan(0);

    // The blank lines are the tell: the plain sink emits the summary as a bare
    // line, the TUI pads it.
    expect(lines[summary - 1], "the TUI puts a blank line before the resource summary").toBe("");
    expect(lines[summary + 1], "the TUI puts a blank line after the resource summary").toBe("");

    expect(term.output()).toContain("my-test-bucket");
    // Still the reduced, externally-managed shape — no container fields.
    expect(term.output()).not.toContain("Container:");
    expect(term.output()).not.toContain("Uptime:");
  });
});
