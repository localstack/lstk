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
 * This is a `#!/bin/sh` script, so it covers macOS and Linux only — there is no
 * Windows equivalent here (see `platform.ts`'s `fakeBrowser` for the same
 * caveat on the same two platforms).
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

// Unit separator: delimits fields within a recorded line. Chosen because it
// cannot appear in a normal argv/env value, so no escaping is needed on write.
const FS = "\x1f";
const BEGIN = "@@lstk-fake-binary-begin@@";
const END = "@@lstk-fake-binary-end@@";

/**
 * Creates a fake executable named `options.name` on its own PATH-ready
 * directory. Prepend `fake.path` onto the environment given to `lstk()`.
 *
 * Caveats inherited from the one-line-per-field log format: an argv or env
 * value containing a newline will corrupt parsing, and env values are read
 * via the `env` builtin, so an embedded newline there breaks it too. Neither
 * comes up in the tool invocations this suite drives (endpoints, credentials,
 * region names, file paths).
 */
export async function fakeBinary(options: FakeBinaryOptions): Promise<FakeBinary> {
  const dir = await mkdtemp(path.join(os.tmpdir(), `lstk-e2e-${options.name}-`));
  onTestFinished(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  const logPath = path.join(dir, "calls.log");
  const scriptPath = path.join(dir, options.name);
  await writeFile(scriptPath, buildScript(options, logPath), { mode: 0o755 });

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

function shQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

function buildCondition(when: string[]): string {
  return when.map((value, i) => `[ "$${i + 1}" = ${shQuote(value)} ]`).join(" && ");
}

function buildScript(options: FakeBinaryOptions, logPath: string): string {
  const lines: string[] = ["#!/bin/sh"];

  lines.push(`LOG=${shQuote(logPath)}`);
  lines.push("{");
  lines.push(`  printf '%s\\n' ${shQuote(BEGIN)}`);
  lines.push(`  printf 'CWD${FS}%s\\n' "$(pwd)"`);
  lines.push(`  for a in "$@"; do printf 'ARG${FS}%s\\n' "$a"; done`);
  lines.push(`  env | while IFS= read -r line; do printf 'ENV${FS}%s\\n' "$line"; done`);
  for (const file of options.captureFiles ?? []) {
    lines.push(
      `  if [ -f ${shQuote(file)} ]; then printf 'FILE${FS}%s${FS}%s\\n' ${shQuote(file)} "$(base64 < ${shQuote(file)} | tr -d '\\n')"; fi`,
    );
  }
  lines.push(`  printf '%s\\n' ${shQuote(END)}`);
  lines.push(`} >> "$LOG"`);
  lines.push("");

  const responses = options.responses ?? [];
  const conditional = responses.filter((r) => r.when && r.when.length > 0);
  const fallback = responses.find((r) => !r.when || r.when.length === 0) ?? { exitCode: 0 };

  for (const rule of conditional) {
    lines.push(`if ${buildCondition(rule.when as string[])}; then`);
    if (rule.stdout) lines.push(`  printf '%s' ${shQuote(rule.stdout)}`);
    if (rule.stderr) lines.push(`  printf '%s' ${shQuote(rule.stderr)} >&2`);
    lines.push(`  exit ${rule.exitCode ?? 0}`);
    lines.push("fi");
  }
  if (fallback.stdout) lines.push(`printf '%s' ${shQuote(fallback.stdout)}`);
  if (fallback.stderr) lines.push(`printf '%s' ${shQuote(fallback.stderr)} >&2`);
  lines.push(`exit ${fallback.exitCode ?? 0}`);

  return `${lines.join("\n")}\n`;
}

async function readCalls(logPath: string): Promise<FakeCall[]> {
  let content: string;
  try {
    content = await readFile(logPath, "utf8");
  } catch {
    return [];
  }

  const calls: FakeCall[] = [];
  for (const block of content.split(`${BEGIN}\n`).slice(1)) {
    const body = block.split(`${END}\n`)[0] ?? "";
    let cwd = "";
    const args: string[] = [];
    const env: Record<string, string> = {};
    const files: Record<string, string> = {};

    for (const line of body.split("\n")) {
      if (line.length === 0) continue;
      const sep = line.indexOf(FS);
      if (sep === -1) continue;
      const kind = line.slice(0, sep);
      const rest = line.slice(sep + 1);

      if (kind === "CWD") {
        cwd = rest;
      } else if (kind === "ARG") {
        args.push(rest);
      } else if (kind === "ENV") {
        const eq = rest.indexOf("=");
        if (eq !== -1) env[rest.slice(0, eq)] = rest.slice(eq + 1);
      } else if (kind === "FILE") {
        const sep2 = rest.indexOf(FS);
        if (sep2 !== -1) {
          const filePath = rest.slice(0, sep2);
          files[filePath] = Buffer.from(rest.slice(sep2 + 1), "base64").toString("utf8");
        }
      }
    }

    calls.push({ args, env, cwd, files });
  }
  return calls;
}
