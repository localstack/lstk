import { describe, expect, test } from "vitest";
import { lstk, parseEnvelope, tempHome } from "../support/index.ts";

// Ported from test/integration/json_flag_test.go.
//
// Most commands have not opted into --json yet, so the rejection gate is the one
// guaranteed-universal response: it is itself rendered as an envelope on stdout.

describe("lstk --json", () => {
  test("rejects a command that has not opted in, as an envelope on stdout", async () => {
    const home = await tempHome();

    const run = await lstk(["status", "--json"], { home });

    expect(run).toExitWith(1);
    const envelope = parseEnvelope(run.stdout);
    expect(envelope).toMatchObject({
      command: "status",
      status: "error",
      error: { code: "NOT_JSON_CAPABLE" },
    });
    expect(envelope.error?.message).toContain("status");
    expect(run.stderr, "the rejection is JSON on stdout, not plain text on stderr").toBe("");
  });

  test("rejects the bare-root start behaviour and names it 'start'", async () => {
    const home = await tempHome();

    const run = await lstk(["--json"], { home });

    expect(run).toExitWith(1);
    expect(parseEnvelope(run.stdout)).toMatchObject({
      command: "start",
      status: "error",
      error: { code: "NOT_JSON_CAPABLE" },
    });
    expect(run.stderr).toBe("");
  });
});
