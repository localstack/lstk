import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { onTestFinished } from "vitest";

/**
 * A fake wrapped-tool executable (`aws`, `terraform`, `cdk`, `sam`, `tofu`, ...)
 * placed on PATH ahead of the real one. It never talks to a real backend: every
 * invocation is appended to a call log (argv, env, cwd, and optionally the
 * contents of named files at call time) that the test can read back after
 * `lstk()` returns, and it answers with a small, test-declared set of
 * canned responses.
 *
 * The recording/response logic is a single Node script (Node is guaranteed
 * present -- it is running the tests) shared by both platforms; only the
 * on-PATH launcher differs. On POSIX it is a `#!/usr/bin/env node` file named
 * exactly `options.name` with mode 0o755. On Windows, where a shebang can't
 * make a file executable, it is a same-named `.cmd` shim that forwards `%*`
 * to `node <script>` -- `exec.LookPath` on Windows resolves bare names via
 * PATHEXT, so a `.cmd` in this fixture's directory shadows a real `.exe`
 * elsewhere on PATH the same way the extension `.cmd` shim in
 * `extension-fixture.ts` does.
 */
export interface FakeBinaryOptions {
  /** Executable name to create on PATH, e.g. "aws", "terraform", "tofu". */
  name: string;
  /**
   * Canned responses, tried in order against argv. The first rule whose
   * `when` is a prefix of the actual argv wins; a rule with no `when` (or an
   * empty one) always matches, so put it last as the default. Omit entirely
   * for "record the call, print nothing, exit 0".
   */
  responses?: FakeBinaryResponse[];
  /**
   * Paths, relative to the invocation's working directory, to snapshot at
   * call time and attach to that call's record. Use this for a file the real
   * tool would see only transiently — e.g. a generated override file lstk
   * deletes right after the wrapped tool exits, which would already be gone
   * by the time a test looks for it otherwise. Missing files are skipped, not
   * an error.
   */
  captureFiles?: string[];
}

export interface FakeBinaryResponse {
  /** Argv prefix that must match exactly, e.g. `["providers", "schema"]`. */
  when?: string[];
  /** Text written to stdout. */
  stdout?: string;
  /** Text written to stderr. */
  stderr?: string;
  /** Process exit code. Defaults to 0. */
  exitCode?: number;
}

/** One recorded invocation of the fake binary. */
export interface FakeCall {
  /** Argv, excluding the program name itself. */
  readonly args: string[];
  /** The full environment the invocation ran with. */
  readonly env: Record<string, string>;
  /** Working directory the invocation ran in. */
  readonly cwd: string;
  /** Contents of any `captureFiles` that existed at call time, keyed by the requested relative path. */
  readonly files: Record<string, string>;
}

export interface FakeBinary {
  /** Directory holding the fake executable and its call log. */
  readonly dir: string;
  /** PATH value with this fixture first, so it shadows any real tool of the same name; the inherited PATH follows. */
  readonly path: string;
  /** Every invocation recorded so far, oldest first. */
  calls(): Promise<FakeCall[]>;
  /** The most recent invocation, or undefined if the binary was never called. */
  lastCall(): Promise<FakeCall | undefined>;
}

/** Config the runner script reads back off disk; the part of FakeBinaryOptions it needs, minus `name`. */
interface RunnerConfig {
  responses: FakeBinaryResponse[];
  captureFiles: string[];
}

/**
 * The recorder/responder, run under `node` on both platforms. It writes one
 * JSON object per invocation to `calls.log` (so an argv/env/file value
 * containing a newline is just an escaped character in the JSON, not a
 * parsing hazard), then answers according to `config.json` sitting next to
 * it. `process.argv[1]` is the script's own path on both the POSIX shebang
 * path and the Windows `.cmd` -> `node <script>` path, so it locates its
 * sibling files without needing an out-of-band argument. Written in plain
 * CommonJS (no `import`/`export`) so it runs correctly regardless of
 * extension or any ambient `package.json` "type" field.
 */
const RUNNER_SCRIPT = `
const fs = require("node:fs");
const path = require("node:path");

const selfPath = process.argv[1];
const dir = path.dirname(selfPath);
const config = JSON.parse(fs.readFileSync(path.join(dir, "config.json"), "utf8"));
const logPath = path.join(dir, "calls.log");

const args = process.argv.slice(2);
const cwd = process.cwd();
const env = { ...process.env };
const files = {};
for (const file of config.captureFiles || []) {
  const filePath = path.resolve(cwd, file);
  if (fs.existsSync(filePath)) {
    files[file] = fs.readFileSync(filePath, "utf8");
  }
}

fs.appendFileSync(logPath, JSON.stringify({ cwd, args, env, files }) + "\\n");

const responses = config.responses || [];
const conditional = responses.filter((r) => r.when && r.when.length > 0);
const fallback = responses.find((r) => !r.when || r.when.length === 0) || { exitCode: 0 };
let rule = fallback;
for (const candidate of conditional) {
  if (candidate.when.every((v, i) => args[i] === v)) {
    rule = candidate;
    break;
  }
}

if (rule.stdout) process.stdout.write(rule.stdout);
if (rule.stderr) process.stderr.write(rule.stderr);
process.exit(rule.exitCode || 0);
`;

/**
 * Creates a fake executable named `options.name` on its own PATH-ready
 * directory. Prepend `fake.path` onto the environment given to `lstk()`.
 */
export async function fakeBinary(options: FakeBinaryOptions): Promise<FakeBinary> {
  const dir = await mkdtemp(path.join(os.tmpdir(), `lstk-e2e-${options.name}-`));
  onTestFinished(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  const logPath = path.join(dir, "calls.log");
  const config: RunnerConfig = { responses: options.responses ?? [], captureFiles: options.captureFiles ?? [] };
  await writeFile(path.join(dir, "config.json"), JSON.stringify(config));

  if (process.platform === "win32") {
    // A .cmd can't carry a shebang, so it can't run RUNNER_SCRIPT directly;
    // it re-invokes it through node instead. `%*` forwards this shim's own
    // argv verbatim (see the module doc for why that preserves quoting,
    // spaces, and `=` in arguments across the round trip).
    const scriptPath = path.join(dir, `${options.name}.cjs`);
    await writeFile(scriptPath, RUNNER_SCRIPT);
    await writeFile(path.join(dir, `${options.name}.cmd`), `@echo off\r\nnode "${scriptPath}" %*\r\n`);
  } else {
    // The shebang line is a comment as far as `node` is concerned, so this
    // same script content runs fine when node is invoked on it explicitly too.
    const scriptPath = path.join(dir, options.name);
    await writeFile(scriptPath, `#!/usr/bin/env node\n${RUNNER_SCRIPT}`, { mode: 0o755 });
  }

  return {
    dir,
    path: `${dir}${path.delimiter}${process.env.PATH ?? ""}`,
    async calls() {
      return readCalls(logPath);
    },
    async lastCall() {
      const all = await readCalls(logPath);
      return all[all.length - 1];
    },
  };
}

async function readCalls(logPath: string): Promise<FakeCall[]> {
  let content: string;
  try {
    content = await readFile(logPath, "utf8");
  } catch {
    return [];
  }

  return content
    .split("\n")
    .filter((line) => line.length > 0)
    .map((line) => JSON.parse(line) as FakeCall);
}
