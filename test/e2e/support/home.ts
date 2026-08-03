import { mkdtemp, mkdir, readFile, writeFile, rm, access } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { onTestFinished } from "vitest";
import { lstk } from "./lstk.ts";
import { dockerHost } from "./docker.ts";

/**
 * An isolated HOME for one test: its own config dir, cache dir and file-based
 * keyring. Nothing here touches the developer's real ~/.config/lstk, ~/.aws or
 * ~/.cache/lstk, and two tests can never see each other's state.
 */
export interface Home {
  /** Filesystem path used as HOME (and USERPROFILE on Windows). */
  readonly path: string;
  /** The full, isolated environment handed to the binary. */
  readonly env: Record<string, string>;
  /** Config file path as lstk itself resolves it (`lstk config path`). */
  configPath(): Promise<string>;
  configExists(): Promise<boolean>;
  writeConfig(toml: string): Promise<string>;
  readConfig(): Promise<string>;
}

export interface TempHomeOptions {
  /**
   * Pre-create `$HOME/.config` so config resolution lands on
   * `$HOME/.config/lstk` on both macOS and Linux (default).
   * Set false to exercise the OS-default branch instead.
   */
  xdgConfigDir?: boolean;
  /**
   * Token storage backend. "file" (default) keeps everything inside this home.
   * "system" lets the binary use the real OS keyring — see realKeyringAllowed().
   */
  keyring?: "file" | "system";
  /** Extra env vars for every command run against this home. */
  env?: Record<string, string>;
}

/**
 * Whether tests may use the real OS keyring.
 *
 * The service and account the binary stores under are hardcoded
 * (internal/auth/token_storage.go), so there is exactly one slot per machine: a
 * test that logs in overwrites the developer's own credential, and the logout it
 * asserts on then deletes it. Opt in explicitly with LSTK_E2E_REAL_KEYRING=1, or
 * let CI (disposable runners) do it.
 */
export function realKeyringAllowed(): boolean {
  return process.env.LSTK_E2E_REAL_KEYRING === "1" || process.env.CI === "true";
}

/** Env vars that must survive into the child for the binary to work at all. */
const PASSTHROUGH = [
  "PATH",
  "SHELL",
  "TMPDIR",
  "LANG",
  "LC_ALL",
  // Container runtime discovery (Rancher Desktop, Colima, remote daemons, ...).
  "DOCKER_HOST",
  "DOCKER_CONTEXT",
  "DOCKER_CONFIG",
  "DOCKER_TLS_VERIFY",
  "DOCKER_CERT_PATH",
  // Windows essentials. A process spawned without SystemRoot/ComSpec/TEMP on
  // Windows fails before it reaches main, and ConPTY needs them too.
  "SystemRoot",
  "SystemDrive",
  "ComSpec",
  "PATHEXT",
  "ProgramData",
  "ProgramFiles",
  "ProgramFiles(x86)",
  "windir",
  "TEMP",
  "TMP",
  "USERNAME",
  "COMPUTERNAME",
  "NUMBER_OF_PROCESSORS",
  "PROCESSOR_ARCHITECTURE",
];

/**
 * A closed local port: the binary under test must never reach the production
 * analytics backend, or CI runs would pollute it with fake "start" events.
 */
const UNREACHABLE_ANALYTICS_ENDPOINT = "http://127.0.0.1:1";

export async function tempHome(options: TempHomeOptions = {}): Promise<Home> {
  const root = await mkdtemp(path.join(os.tmpdir(), "lstk-e2e-"));
  onTestFinished(async () => {
    await rm(root, { recursive: true, force: true });
  });

  if (options.xdgConfigDir ?? true) {
    await mkdir(path.join(root, ".config"), { recursive: true });
  }

  const env: Record<string, string> = {};
  for (const key of PASSTHROUGH) {
    const value = process.env[key];
    if (value !== undefined) env[key] = value;
  }

  env.HOME = root;
  if (process.platform === "win32") {
    env.USERPROFILE = root;
    env.APPDATA = path.join(root, "AppData", "Roaming");
    env.LOCALAPPDATA = path.join(root, "AppData", "Local");
  }

  // Keep credentials inside this home unless a test explicitly wants the real
  // keyring: the system store is shared machine state, and on macOS it prompts.
  if ((options.keyring ?? "file") === "file") {
    env.LSTK_KEYRING = "file";
  }
  env.LSTK_ANALYTICS_ENDPOINT = UNREACHABLE_ANALYTICS_ENDPOINT;
  env.LOCALSTACK_DISABLE_EVENTS = "1";
  // An enabled `az` spawns a background uploader that keeps a handle on the
  // temp dir, which breaks cleanup on Windows.
  env.AZURE_CORE_COLLECT_TELEMETRY = "false";

  // See dockerHost(): the runtime's socket usually lives under the real home.
  const host = await dockerHost();
  if (host) env.DOCKER_HOST = host;

  Object.assign(env, options.env ?? {});

  let cachedConfigPath: string | undefined;

  const home: Home = {
    path: root,
    env,

    async configPath() {
      if (cachedConfigPath) return cachedConfigPath;
      const run = await lstk(["config", "path"], { home });
      if (run.exitCode !== 0) {
        throw new Error(`lstk config path failed (${run.exitCode}): ${run.stderr}`);
      }
      cachedConfigPath = run.stdout;
      return cachedConfigPath;
    },

    async configExists() {
      try {
        await access(await home.configPath());
        return true;
      } catch {
        return false;
      }
    },

    async writeConfig(toml: string) {
      const file = await home.configPath();
      await mkdir(path.dirname(file), { recursive: true });
      await writeFile(file, toml);
      return file;
    },

    async readConfig() {
      return readFile(await home.configPath(), "utf8");
    },
  };

  return home;
}
