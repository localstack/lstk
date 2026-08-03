import { chmod, mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

/**
 * A minimal lstk extension, used only to prove lstk's own JSON/non-interactive
 * context conveyance (LSTK_EXT_CONTEXT) — not a stand-in for the fake proxy
 * binaries used by the aws/terraform/cdk/sam tests. It echoes its argv and the
 * conveyed context in the same line-oriented shape the Go suite's reference
 * extension (test/integration/test-samples/extensions/lstk-ref) uses, so
 * assertions read the same way, but is a plain Node script rather than a
 * compiled Go binary — nothing here builds or imports lstk itself.
 */
const EXTENSION_SCRIPT = `
const args = process.argv.slice(2);
let ctx = {};
try { ctx = JSON.parse(process.env.LSTK_EXT_CONTEXT || "{}"); } catch {}
console.log(\`ARGS=[\${args.join(" ")}]\`);
console.log(\`JSON=\${ctx.json === true}\`);
console.log(\`NON_INTERACTIVE=\${ctx.nonInteractive === true}\`);
`;

/**
 * Installs an executable named `lstk-<name>` into `dir`, so that placing `dir`
 * on PATH makes lstk resolve and dispatch to it as the `<name>` extension.
 */
export async function installExtension(dir: string, name: string): Promise<void> {
  await mkdir(dir, { recursive: true });

  if (process.platform === "win32") {
    // lstk resolves extensions via exec.LookPath, which on Windows searches
    // PATHEXT against the exact base name — a .cmd shim re-invoking the script
    // through node satisfies that.
    const scriptPath = path.join(dir, `lstk-${name}.mjs`);
    await writeFile(scriptPath, EXTENSION_SCRIPT);
    await writeFile(path.join(dir, `lstk-${name}.cmd`), `@echo off\r\nnode "${scriptPath}" %*\r\n`);
    return;
  }

  // On Unix, exec.LookPath matches the base name exactly (no extension), so
  // the executable itself must be literally named `lstk-<name>`; the shebang
  // line is what makes it run under node despite carrying no .mjs suffix.
  const execPath = path.join(dir, `lstk-${name}`);
  await writeFile(execPath, `#!/usr/bin/env node\n${EXTENSION_SCRIPT}`);
  await chmod(execPath, 0o755);
}
