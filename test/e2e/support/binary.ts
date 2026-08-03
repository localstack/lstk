import { existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));

/** Absolute path to the built binary under test. Nothing else in this suite knows where it lives. */
export const lstkBinary = path.resolve(
  here,
  "../../../bin",
  process.platform === "win32" ? "lstk.exe" : "lstk",
);

export function binaryExists(): boolean {
  return existsSync(lstkBinary);
}
