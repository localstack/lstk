import { execa } from "execa";
import { lstkBinary } from "./binary.ts";
import type { Home } from "./home.ts";

export interface RunOptions {
  /** Isolated HOME to run against. Always pass one — see support/home.ts. */
  home?: Home;
  /** Working directory. Defaults to the isolated home. */
  cwd?: string;
  /** Extra env vars on top of the home's environment. */
  env?: Record<string, string>;
  /** Text piped to the binary's stdin. */
  stdin?: string;
  timeout?: number;
}

export interface RunResult {
  /** The invocation, for assertion messages: `lstk start --type aws`. */
  readonly command: string;
  readonly stdout: string;
  readonly stderr: string;
  readonly exitCode: number;
}

/**
 * Runs the built lstk binary with no terminal attached, which is how CI and
 * pipelines invoke it. Never throws on a non-zero exit — the exit code is part
 * of what tests assert on. For the interactive TUI paths use `lstkPty`.
 */
export async function lstk(args: string[], options: RunOptions = {}): Promise<RunResult> {
  const { home } = options;
  const env = { ...(home?.env ?? {}), ...(options.env ?? {}) };

  const result = await execa(lstkBinary, args, {
    cwd: options.cwd ?? home?.path,
    env,
    extendEnv: false,
    reject: false,
    timeout: options.timeout,
    input: options.stdin,
    stripFinalNewline: true,
  });

  return {
    command: `lstk ${args.join(" ")}`,
    stdout: (result.stdout ?? "").trim(),
    stderr: (result.stderr ?? "").trim(),
    exitCode: result.exitCode ?? (result.failed ? 1 : 0),
  };
}
