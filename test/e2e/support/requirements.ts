/**
 * Prerequisites that some tests need from the machine they run on.
 *
 * A missing prerequisite skips the affected tests, so a contributor without (say)
 * a container runtime can still run everything else. That leniency is also how
 * coverage erodes unnoticed, so CI sets LSTK_E2E_REQUIRE_ALL=1 on the leg that has
 * everything: there, a missing prerequisite fails the run instead of skipping it.
 *
 * The PTY binding is deliberately NOT expressed here — it is a hard dependency,
 * imported statically in support/pty.ts.
 */
const STRICT = process.env.LSTK_E2E_REQUIRE_ALL === "1";

/**
 * Declares a prerequisite and returns whether tests depending on it must be
 * skipped. Throws in strict mode, at collection time, naming the fix.
 */
export function requirement(name: string, available: boolean, fix: string): boolean {
  if (available) return false;
  if (STRICT) {
    throw new Error(
      `missing prerequisite: ${name}\n` +
        `${fix}\n` +
        `(LSTK_E2E_REQUIRE_ALL=1 turns skipped prerequisites into failures)`,
    );
  }
  return true;
}
