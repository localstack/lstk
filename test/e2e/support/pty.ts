import { lstkBinary } from "./binary.ts";
import type { Home } from "./home.ts";
import { onTestFinished } from "vitest";

/**
 * The PTY binding is a required dependency, imported statically: a machine that
 * cannot provide it must fail loudly at install or import, never quietly turn
 * every terminal test into a skip. See README "Terminal tests".
 *
 * Windows is included — node-pty drives ConPTY there, unlike the Go suite's
 * creack/pty, which has no Windows support and skips every TUI test.
 */
import { spawn as spawnPty } from "node-pty";

/** CSI and OSC sequences plus the two-character escapes a repaint emits. */
const ANSI = new RegExp(
  [
    "\\u001b\\][^\\u0007\\u001b]*(?:\\u0007|\\u001b\\\\)?", // OSC ... BEL / ST
    "[\\u001b\\u009b][[\\]()#;?]*(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PR-TZcf-nq-uy=><~]",
    "\\u001b[()#][A-Za-z0-9]",
    "\\u001b[=>78]",
  ].join("|"),
  "g",
);

/**
 * Reduces a terminal repaint to what a human would read: escape sequences gone,
 * CRLF (which ConPTY always emits) normalised, trailing padding dropped.
 */
export function stripAnsi(text: string): string {
  return text
    .replace(ANSI, "")
    .replace(/\r\n/g, "\n")
    .replace(/[ \t]+$/gm, "");
}

const KEYS = {
  enter: "\r",
  up: "\u001b[A",
  down: "\u001b[B",
  space: " ",
  tab: "\t",
  esc: "\u001b",
  "ctrl-c": "\u0003",
} as const;

export type Key = keyof typeof KEYS;

export interface Terminal {
  /** Everything printed so far, with ANSI escape sequences removed. */
  output(): string;
  /** Resolves once `needle` shows up; rejects with the output so far on timeout. */
  waitFor(needle: string | RegExp, options?: { timeout?: number }): Promise<void>;
  /** Fails if `needle` shows up within the window. */
  expectNever(needle: string | RegExp, options?: { within?: number }): Promise<void>;
  press(key: Key): void;
  type(text: string): void;
  /** Resolves with the exit code once the process ends. */
  exitCode(): Promise<number>;
  kill(): void;
}

export interface PtyOptions {
  home: Home;
  cwd?: string;
  env?: Record<string, string>;
  cols?: number;
  rows?: number;
}

/**
 * Runs lstk on a pseudo-terminal, which is what makes it take its interactive
 * path (Bubble Tea TUI, prompts, spinners) instead of the plain-sink path.
 */
export function lstkPty(args: string[], options: PtyOptions): Terminal {
  const child = spawnPty(lstkBinary, args, {
    cwd: options.cwd ?? options.home.path,
    env: { ...options.home.env, ...(options.env ?? {}) },
    cols: options.cols ?? 120,
    rows: options.rows ?? 40,
    name: "xterm-256color",
  });

  let buffer = "";
  child.onData((chunk) => {
    buffer += chunk;
  });

  let exit: { code: number } | undefined;
  const exited = new Promise<number>((resolve) => {
    child.onExit(({ exitCode }) => {
      exit = { code: exitCode };
      resolve(exitCode);
    });
  });

  let killed = false;
  const kill = () => {
    if (killed || exit) return;
    killed = true;
    try {
      child.kill();
    } catch {
      // Already gone.
    }
  };
  onTestFinished(kill);

  const output = () => stripAnsi(buffer);
  const matches = (needle: string | RegExp) =>
    typeof needle === "string" ? output().includes(needle) : needle.test(output());

  const terminal: Terminal = {
    output,

    async waitFor(needle, { timeout = 15_000 } = {}) {
      const deadline = Date.now() + timeout;
      while (Date.now() < deadline) {
        if (matches(needle)) return;
        await sleep(100);
      }
      throw new Error(
        `timed out after ${timeout}ms waiting for ${describe(needle)}\n--- terminal output ---\n${output()}`,
      );
    },

    async expectNever(needle, { within = 2_000 } = {}) {
      const deadline = Date.now() + within;
      while (Date.now() < deadline) {
        if (matches(needle)) {
          throw new Error(
            `${describe(needle)} appeared but should not have\n--- terminal output ---\n${output()}`,
          );
        }
        await sleep(100);
      }
    },

    press(key) {
      child.write(KEYS[key]);
    },

    type(text) {
      child.write(text);
    },

    exitCode: () => exited,
    kill,
  };

  return terminal;
}

function describe(needle: string | RegExp): string {
  return typeof needle === "string" ? JSON.stringify(needle) : String(needle);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
