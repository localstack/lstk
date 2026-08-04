import { execa } from "execa";
import { onTestFinished } from "vitest";
import { docker } from "./docker.ts";

/**
 * A stand-in for a running emulator, cheap enough to use in any test that needs
 * one without a license or a real image.
 *
 * `internal/container/running.go`'s ResolveRunningContainerName looks the emulator
 * up by container name first (`localstack-<type>`), falling back to (known image
 * repo, internal port) for containers started outside lstk. Neither path requires
 * a working LocalStack, so a plain image that just stays up reads as "the emulator
 * is running" to `lstk stop` / `status` / `restart` / `logs` / `reset`. Mirrors
 * `startTestContainer` / `startNamedTestContainer` / `startExternalContainer` in
 * test/integration/main_test.go.
 *
 * What this does NOT provide is a responding emulator API: tests that need
 * /_localstack/... to answer point lstk at a local HTTP server via LOCALSTACK_HOST.
 */
const STAND_IN_IMAGE = "alpine:latest";

/**
 * Stays up until asked to stop, and then stops immediately.
 *
 * A plain `sleep infinity` ignores SIGTERM, so `lstk stop` — which stops the
 * container the polite way — waited out Docker's full 10s grace period before the
 * SIGKILL, costing ~9s per stop test for nothing. Trapping TERM makes the same test
 * ~1s. PID 1 is still a shell, which is what `writeContainerLogLines` needs in order
 * to write to /proc/1/fd/1.
 */
const STAY_UP = ["sh", "-c", 'trap "exit 0" TERM; while :; do sleep 1; done'];

/** The default AWS emulator's canonical container name (tag "latest"). */
export const defaultEmulatorName = "localstack-aws";

let stubCounter = 0;

export interface PrivateEmulator {
  /** Non-"latest" tag, which is what makes the container name unique. */
  readonly tag: string;
  /** Container name lstk derives from that tag: `localstack-<type>-<tag>`. */
  readonly name: string;
  /** config.toml body selecting this emulator. */
  readonly config: string;
}

/**
 * An emulator identity no other test shares, so the test needs no global lock.
 *
 * `config.ContainerConfig.Name()` returns `localstack-<type>` only for tag "latest";
 * any other tag yields `localstack-<type>-<tag>`. Giving each test its own tag
 * therefore gives it its own container name, and lstk's name-first discovery finds
 * exactly that container — so tests that only need "an emulator is running" can run
 * concurrently instead of queueing behind `useExclusiveEmulator()`.
 *
 * The port stays at the caller's choice (4566 by default): stub containers publish
 * nothing, and the image/port fallback only matches real `localstack/*` image
 * references, so a shared port cannot cross-match a plain stand-in container. Tests
 * that deliberately exercise that fallback — or that must use the canonical
 * `localstack-<type>` name — still need the lock.
 *
 * Just as useful for asserting the *absence* of an emulator: write the config and
 * never start a stub for it, and "not running" holds no matter what any concurrent
 * test is doing. That is stronger than the lock, which only kept other well-behaved
 * tests away rather than guaranteeing the name was free.
 */
export function privateEmulator(
  type: "aws" | "snowflake" | "azure" = "aws",
  options: { port?: string } = {},
): PrivateEmulator {
  const tag = `e2e-${process.pid}-${++stubCounter}`;
  const port = options.port ?? "4566";
  return {
    tag,
    name: `localstack-${type}-${tag}`,
    config: `[[containers]]\ntype = "${type}"\ntag = "${tag}"\nport = "${port}"\n`,
  };
}

export interface StubEmulatorOptions {
  /**
   * Image to run in place of a real emulator image. Defaults to `alpine:latest`,
   * which is pulled automatically. Pass an already-tagged image (e.g. via
   * `docker.tag(...)` onto a known emulator repo like
   * `localstack/localstack-pro:<tag>`) to exercise the image-based fallback used
   * for containers not named `localstack-<type>`; a caller-supplied image is never
   * pulled, since it usually exists only locally.
   */
  image?: string;
  /**
   * Publishes container port 4566/tcp to this host port, on 127.0.0.1 unless
   * `hostIp` names another address. A loopback alias such as 127.0.0.2 frees the
   * same port number on 127.0.0.1 for a mock server — see
   * `dockerCanBindLoopbackAlias`, since not every daemon allows it.
   */
  hostBinding?: { hostPort: string; hostIp?: string };
  /** Extra arguments for `docker run`, inserted before the image. */
  dockerArgs?: string[];
}

/**
 * Starts a placeholder container under `name`, removed when the test finishes.
 *
 * Any container already holding the name is force-removed first: the name is fixed
 * by lstk's discovery rules, so a crash between creation and cleanup — or a stray
 * container from outside this suite — would otherwise block every later run.
 */
export async function startStubEmulator(
  name: string = defaultEmulatorName,
  options: StubEmulatorOptions = {},
): Promise<void> {
  const image = options.image ?? STAND_IN_IMAGE;
  if (image === STAND_IN_IMAGE) {
    await docker.pull(image);
  }

  await docker.removeContainer(name);
  onTestFinished(async () => {
    await docker.removeContainer(name);
  });

  const args = ["run", "-d", "--name", name, ...(options.dockerArgs ?? [])];
  if (options.hostBinding) {
    const hostIp = options.hostBinding.hostIp ?? "127.0.0.1";
    args.push("-p", `${hostIp}:${options.hostBinding.hostPort}:4566`);
  }
  args.push(image, ...STAY_UP);

  const result = await execa("docker", args, { reject: false });
  if (result.exitCode !== 0) {
    throw new Error(`docker run --name ${name} failed: ${result.stderr}`);
  }
}

/**
 * Writes `lines` to the container's PID 1 stdout (so they show up in `docker logs`,
 * the same channel `lstk logs` reads) and waits until the last one is visible, so
 * tail/follow assertions never race the write.
 */
export async function writeContainerLogLines(name: string, lines: string[]): Promise<void> {
  const lastLine = lines.at(-1);
  if (lastLine === undefined) return;

  const script = lines.map((line) => `echo '${line.replaceAll("'", `'\\''`)}'`).join("; ");
  await execa("docker", ["exec", name, "sh", "-c", `{ ${script}; } >/proc/1/fd/1`]);
  await waitForLogLine(name, lastLine);
}

async function waitForLogLine(name: string, marker: string, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const result = await execa("docker", ["logs", name], { reject: false });
    const combined = `${result.stdout ?? ""}\n${result.stderr ?? ""}`;
    if (combined.includes(marker)) return;
    if (Date.now() > deadline) {
      throw new Error(`"${marker}" never appeared in docker logs ${name}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}
