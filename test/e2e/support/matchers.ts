import { expect } from "vitest";
import type { RunResult } from "./lstk.ts";

/**
 * Matchers that keep the "then" half of a test one line long and make failures
 * self-explanatory: every message includes the invocation and both streams.
 */
expect.extend({
  toSucceed(received: RunResult) {
    return {
      pass: received.exitCode === 0,
      message: () => `${describeRun(received)}\nexpected exit code 0, got ${received.exitCode}`,
      actual: received.exitCode,
      expected: 0,
    };
  },

  toFail(received: RunResult) {
    return {
      pass: received.exitCode !== 0,
      message: () => `${describeRun(received)}\nexpected a non-zero exit code`,
    };
  },

  toExitWith(received: RunResult, expected: number) {
    return {
      pass: received.exitCode === expected,
      message: () =>
        `${describeRun(received)}\nexpected exit code ${expected}, got ${received.exitCode}`,
      actual: received.exitCode,
      expected,
    };
  },

  toPrint(received: RunResult, expected: string | RegExp) {
    const combined = `${received.stdout}\n${received.stderr}`;
    const pass =
      typeof expected === "string" ? combined.includes(expected) : expected.test(combined);
    return {
      pass,
      message: () => `${describeRun(received)}\nexpected output to ${pass ? "not " : ""}contain ${expected}`,
    };
  },

  /**
   * Exact-match a stream against an authored block of expected output.
   *
   * The expected text is dedented — leading and trailing blank lines dropped, the
   * common indentation stripped — so the block can sit at the indentation of the
   * surrounding code while still asserting the output byte for byte. Indentation
   * *within* the block survives, which is what makes lstk's nested "==>" action
   * lines assertable.
   *
   * Preferred over an inline snapshot for CLI output: the expectation is written
   * deliberately rather than recorded, so no `--update` run can quietly bless a
   * regression, and it composes with `test.each`, which inline snapshots reject.
   */
  toPrintExactly(received: string, expected: string) {
    const want = dedent(expected);
    return {
      pass: received === want,
      message: () =>
        `expected the stream to be exactly:\n${want || "(empty)"}\n\nbut it was:\n${received || "(empty)"}`,
      actual: received,
      expected: want,
    };
  },
});

/**
 * Strips the indentation an authored template literal picks up from the code
 * around it: blank first/last lines go, then the smallest indent shared by the
 * remaining non-empty lines is removed from every line.
 */
function dedent(text: string): string {
  const lines = text.split("\n");
  while (lines.length > 0 && lines[0]?.trim() === "") lines.shift();
  while (lines.length > 0 && lines.at(-1)?.trim() === "") lines.pop();

  const indents = lines
    .filter((line) => line.trim() !== "")
    .map((line) => line.length - line.trimStart().length);
  const common = indents.length > 0 ? Math.min(...indents) : 0;

  return lines.map((line) => line.slice(common)).join("\n");
}

function describeRun(run: RunResult): string {
  return [
    `$ ${run.command}`,
    `--- stdout ---\n${run.stdout || "(empty)"}`,
    `--- stderr ---\n${run.stderr || "(empty)"}`,
  ].join("\n");
}

interface CliMatchers<R = unknown> {
  toSucceed(): R;
  toFail(): R;
  toExitWith(code: number): R;
  toPrint(expected: string | RegExp): R;
  /** On a stream (`run.stdout` / `run.stderr`): exact match against dedented text. */
  toPrintExactly(expected: string): R;
}

declare module "vitest" {
  interface Assertion<T = any> extends CliMatchers<T> {}
  interface AsymmetricMatchersContaining extends CliMatchers {}
}
