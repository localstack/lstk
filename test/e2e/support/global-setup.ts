import { binaryExists, lstkBinary } from "./binary.ts";

/**
 * Fails the whole run, once, on anything the suite cannot work without — rather
 * than letting each test file discover it separately.
 */
export default async function setup() {
  if (!binaryExists()) {
    throw new Error(
      `lstk binary not found at ${lstkBinary}\nBuild it first:\n\n  make build\n`,
    );
  }

  // The PTY binding is a native module and roughly half the suite depends on it.
  // It must never be allowed to degrade into skipped tests, so check it up front
  // and explain the usual causes.
  try {
    const pty = await import("node-pty");
    if (typeof pty.spawn !== "function") {
      throw new Error("module loaded but exposes no spawn()");
    }
  } catch (cause) {
    throw new Error(
      "the PTY binding could not be loaded, so terminal tests cannot run.\n" +
        `  cause: ${cause instanceof Error ? cause.message.split("\n")[0] : String(cause)}\n` +
        "It is a native module. Usual causes, in order of likelihood:\n" +
        "  - install scripts were blocked: check pnpm-workspace.yaml's allowBuilds,\n" +
        "    then re-run `pnpm install`\n" +
        "  - the node-pty version was changed: 1.1.0 ships a non-executable\n" +
        "    spawn-helper (macOS) and no Linux prebuilds. The pin is exact for that\n" +
        "    reason — see README 'Terminal tests'\n" +
        "  - musl-based image (Alpine): the prebuilds are glibc-only, so it must\n" +
        "    compile, which needs python3, make and a C++ compiler\n",
      { cause },
    );
  }
}
