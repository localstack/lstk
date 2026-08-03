import { access } from "node:fs/promises";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { lstk, tempHome } from "../support/index.ts";

// Ported from test/integration/docs_test.go.

async function fileExists(filePath: string): Promise<boolean> {
  try {
    await access(filePath);
    return true;
  } catch {
    return false;
  }
}

describe("lstk docs", () => {
  test("generates man pages", async () => {
    const home = await tempHome();
    const dir = path.join(home.path, "manpages");

    const run = await lstk(["docs", "--format", "man", "--dir", dir], { home });

    expect(run).toSucceed();
    // The generated man pages themselves are files on disk, not CLI output --
    // checked below by existence, not content. What belongs here is the
    // command's own stdout/stderr: a quiet success with nothing printed.
    expect(run.stdout).toPrintExactly("");
    expect(run.stderr).toPrintExactly("");
    expect(await fileExists(path.join(dir, "lstk.1"))).toBe(true);
    expect(await fileExists(path.join(dir, "lstk-start.1"))).toBe(true);
    expect(await fileExists(path.join(dir, "lstk-stop.1"))).toBe(true);
  });

  test("generates markdown", async () => {
    const home = await tempHome();
    const dir = path.join(home.path, "markdown");

    const run = await lstk(["docs", "--format", "markdown", "--dir", dir], { home });

    expect(run).toSucceed();
    // Same reasoning as the man-page test above: the generated markdown files
    // are checked by existence, not content; only the command's own
    // stdout/stderr is CLI output worth snapshotting.
    expect(run.stdout).toPrintExactly("");
    expect(run.stderr).toPrintExactly("");
    expect(await fileExists(path.join(dir, "lstk.md"))).toBe(true);
    expect(await fileExists(path.join(dir, "lstk_start.md"))).toBe(true);
    expect(await fileExists(path.join(dir, "lstk_stop.md"))).toBe(true);
  });

  test("rejects an invalid format", async () => {
    const home = await tempHome();
    const dir = path.join(home.path, "invalid");

    const run = await lstk(["docs", "--format", "invalid", "--dir", dir], { home });

    expect(run).toFail();
    expect(run.stderr).toPrintExactly("Error: unsupported format: invalid (use 'man' or 'markdown')");
  });

  test("is hidden from --help", async () => {
    const home = await tempHome();

    const run = await lstk(["--help"], { home });

    expect(run).toSucceed();
    // Not snapshotted: root --help lists every top-level command, so a full
    // snapshot here would flap on every unrelated command addition. The
    // point of this test is narrower -- "docs" specifically must not appear
    // -- which an absence substring check states directly.
    expect(run).not.toPrint("docs");
  });
});
