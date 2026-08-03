import { access, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { execa } from "execa";
import { describe, expect, test, onTestFinished } from "vitest";
import { lstk, tempHome, requirement } from "../support/index.ts";
import { lstkBinary } from "../support/binary.ts";

// Ported from test/integration/completion_test.go.
//
// Guards DEVX-950: stock macOS ships bash 3.2 with no bash-completion package,
// so the CLAUDE.md "Shell Completion" section requires the generated script to
// be self-contained, and warns that `source <(lstk completion bash)` silently
// no-ops on bash 3.2 — `eval "$(lstk completion bash)"` must be used instead.
// These tests drive the generated script inside a bare `bash --noprofile
// --norc` (no bash-completion, no developer rc files) so nothing on this
// machine can mask a regression.

async function findBash(): Promise<string | undefined> {
  if (process.platform === "win32") return undefined;
  try {
    await access("/bin/bash");
    return "/bin/bash";
  } catch {
    // fall through to a PATH lookup below.
  }
  try {
    const { stdout } = await execa("which", ["bash"]);
    return stdout.trim() || undefined;
  } catch {
    return undefined;
  }
}

const bashPath = await findBash();
const noBash = requirement(
  "a bash shell (bash completion is not applicable on Windows)",
  bashPath !== undefined,
  "Run these tests on macOS or Linux with bash installed.",
);

interface DriverResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

/**
 * Generates the real completion script via the built binary, then runs a
 * driver script against it in a bash with almost nothing on PATH — just the
 * built binary's own directory plus /usr/bin and /bin, so the script cannot
 * accidentally reach a different `lstk` (e.g. one installed via Homebrew) or
 * the developer's bash-completion package. Mirrors
 * test/integration/completion_test.go's runBashCompletionDriver.
 */
async function runCompletionDriver(driver: string): Promise<DriverResult> {
  const home = await tempHome();
  const genRun = await lstk(["completion", "bash"], { home });
  if (genRun.exitCode !== 0) {
    throw new Error(`lstk completion bash failed: ${genRun.stderr}`);
  }

  const dir = await mkdtemp(path.join(os.tmpdir(), "lstk-e2e-completion-"));
  onTestFinished(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  const scriptPath = path.join(dir, "lstk-completion.bash");
  await writeFile(scriptPath, genRun.stdout);
  const driverPath = path.join(dir, "driver.bash");
  await writeFile(driverPath, driver);

  const binDir = path.dirname(lstkBinary);
  const result = await execa(bashPath as string, ["--noprofile", "--norc", driverPath, scriptPath], {
    cwd: dir,
    env: {
      HOME: home.path,
      PATH: `${binDir}:/usr/bin:/bin`,
      LSTK_KEYRING: "file",
    },
    extendEnv: false,
    reject: false,
  });

  return {
    stdout: (result.stdout ?? "").toString().trim(),
    stderr: withoutDriverArtifacts((result.stderr ?? "").toString()),
    exitCode: result.exitCode ?? (result.failed ? 1 : 0),
  };
}

/**
 * Drops warnings caused by this driver rather than by the completion script.
 *
 * The driver calls the completion function directly instead of pressing Tab, so bash
 * is not "executing a completion function" as far as `compopt` is concerned and warns
 * — on Linux, where bash has `compopt` at all; macOS bash 3.2 has no such builtin, so
 * the warning never appears there. Real Tab-completion never hits this, and filtering
 * it keeps the rest of stderr asserted, so a genuine script error still fails.
 */
function withoutDriverArtifacts(stderr: string): string {
  return stderr
    .split("\n")
    .filter((line) => !/compopt: not currently executing completion function/.test(line))
    .join("\n")
    .trim();
}

/**
 * Simulates pressing Tab on the given command-line state and prints the
 * resulting COMPREPLY, one completion per line. compWords is a bash array
 * literal — readline splits at COMP_WORDBREAKS characters ('=' and ':'
 * included), so a typed '--flag=value' must be given as '--flag = value'.
 */
function completeInDriver(compWords: string, cword: number, line: string): string {
  return `source "$1" || exit 1
COMP_WORDS=(${compWords})
COMP_CWORD=${cword}
COMP_LINE=${JSON.stringify(line)}
COMP_POINT=${line.length}
__start_lstk
status=$?
printf '%s\\n' "\${COMPREPLY[@]}"
exit $status
`;
}

describe.skipIf(noBash)("lstk completion bash", () => {
  test("works without the bash-completion package installed (DEVX-950)", async () => {
    const result = await runCompletionDriver(completeInDriver("lstk st", 1, "lstk st"));

    expect(result.exitCode).toBe(0);
    // The candidate list Tab-completion prints (one per line) is the CLI
    // output under test here; snapshotting it also catches any unexpected
    // extra/missing/reordered candidate, not just the three named below.
    expect(result.stdout).toPrintExactly(`
      start
      status
      stop
    `);
    expect(result.stderr).toPrintExactly("");
  });

  test("completes after a whitespace-separated flag value", async () => {
    // 'lstk --config= st<TAB>' delivers the same COMP_WORDS as 'lstk --config=st',
    // and only COMP_LINE reveals that 'st' is a separate word to complete.
    const result = await runCompletionDriver(completeInDriver("lstk --config = st", 3, "lstk --config= st"));

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toPrintExactly(`
      start
      status
      stop
    `);
    expect(result.stderr).toPrintExactly("");
  });

  // Not converted to snapshots: `result.stdout` here is not a completion
  // candidate list a user would see on Tab -- it's a `cur=`/`prev=`/`cword=`/
  // `nwords=` debug printout this test invented to inspect the reassembly
  // driver's internal word-splitting, i.e. structured data encoded as lines
  // rather than CLI output. `toEqual` also states the four fields far more
  // legibly than a blob snapshot would. Separately, `test.each` cannot use
  // inline snapshots at all -- Vitest ties an inline snapshot to its source
  // call site, and a call site invoked once per row has nowhere to hold a
  // different expected value per row ("InlineSnapshot cannot be used inside
  // of test.each or describe.each").
  test.each([
    {
      name: "adjacent pieces re-join",
      compWords: "lstk --config = ./cfg",
      cword: 3,
      line: "lstk --config=./cfg",
      expect: ["cur=--config=./cfg", "prev=lstk", "cword=1", "nwords=2"],
    },
    {
      name: "word after separator-then-space stays separate",
      compWords: "lstk --config = st",
      cword: 3,
      line: "lstk --config= st",
      expect: ["cur=st", "prev=--config=", "cword=2", "nwords=3"],
    },
    {
      name: "whitespace-surrounded separator stays separate",
      compWords: "lstk --config = ./x",
      cword: 3,
      line: "lstk --config = ./x",
      expect: ["cur=./x", "prev==", "cword=3", "nwords=4"],
    },
    {
      name: "empty word after separator-then-space",
      compWords: 'lstk --config = ""',
      cword: 3,
      line: "lstk --config= ",
      expect: ["cur=", "prev=--config=", "cword=2", "nwords=3"],
    },
  ])("reassembles wordbreak splits: $name", async ({ compWords, cword, line, expect: expected }) => {
    const driver = `source "$1" || exit 1
COMP_WORDS=(${compWords})
COMP_CWORD=${cword}
COMP_LINE=${JSON.stringify(line)}
COMP_POINT=${line.length}
run_reassembly() {
    local cur prev words cword
    _get_comp_words_by_ref -n =: cur prev words cword || exit 1
    printf 'cur=%s\\n' "$cur"
    printf 'prev=%s\\n' "$prev"
    printf 'cword=%s\\n' "$cword"
    printf 'nwords=%s\\n' "\${#words[@]}"
}
run_reassembly
`;
    const result = await runCompletionDriver(driver);

    expect(result.exitCode).toBe(0);
    expect(result.stderr).not.toContain("command not found");
    expect(result.stdout.split("\n")).toEqual(expected);
  });

  test("yields to an already-installed bash-completion package", async () => {
    const driver = `_get_comp_words_by_ref() { echo "package version"; }
source "$1" || exit 1
_get_comp_words_by_ref
`;
    const result = await runCompletionDriver(driver);

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toPrintExactly("package version");
  });
});
