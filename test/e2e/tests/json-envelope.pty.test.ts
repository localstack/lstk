import { describe, expect, test } from "vitest";
import { lstkPty, tempHome } from "../support/index.ts";

// Ported from test/integration/json_flag_test.go's TestJSONFlagDoesNotLaunchTUIOnPTY.
//
// Needs a real terminal: on a plain (non-PTY) invocation, start would already
// take the non-interactive path regardless of --json, so this is the one case
// that only proves anything when a terminal is actually attached.

describe("--json on a terminal", () => {
  test("does not launch the interactive TUI", async () => {
    const home = await tempHome();

    const term = lstkPty(["start", "--json"], { home });

    expect(await term.exitCode()).toBe(1);
    expect(term.output()).toContain("start");
    // If the TUI had launched, it would show the auth prompt (start with no
    // auth token requires interactive login) instead of exiting immediately.
    expect(term.output()).not.toContain("Press any key");
  });
});
