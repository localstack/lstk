import { execa } from "execa";
import { mkdir, rm, stat } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterAll, beforeAll, onTestFinished } from "vitest";

/**
 * Docker is driven through the `docker` CLI rather than an API client: the CLI
 * resolves DOCKER_HOST, contexts and non-Docker runtimes exactly the way a user's
 * shell does, and it keeps this suite free of native dependencies.
 */
async function docker_(args: string[]): Promise<{ stdout: string; exitCode: number; stderr: string }> {
  const result = await execa("docker", args, { reject: false });
  return {
    stdout: (result.stdout ?? "").trim(),
    stderr: (result.stderr ?? "").trim(),
    exitCode: result.exitCode ?? 1,
  };
}

let availability: Promise<boolean> | undefined;

/** True when a container runtime is reachable and container tests can run here. */
export function dockerIsAvailable(): Promise<boolean> {
  // GitHub's Windows runners cannot run Linux containers (no nested virtualization).
  if (process.platform === "win32" && process.env.CI) return Promise.resolve(false);
  availability ??= docker_(["info", "--format", "{{.ServerVersion}}"]).then((r) => r.exitCode === 0);
  return availability;
}

let hostEndpoint: Promise<string | undefined> | undefined;

/**
 * The daemon endpoint of the harness's active Docker context.
 *
 * Every isolated home gets this as DOCKER_HOST, because most runtimes put their
 * socket under the user's real home (`~/.docker/run/docker.sock`,
 * `~/.colima/default/docker.sock`, ...) — which the binary can no longer find
 * once HOME points at a temp dir. Resolving it here keeps container tests
 * working on Docker Desktop, Colima, Rancher Desktop and OrbStack alike, and is
 * a no-op on CI where the native `/var/run/docker.sock` is already correct.
 */
export function dockerHost(): Promise<string | undefined> {
  hostEndpoint ??= (async () => {
    if (!(await dockerIsAvailable())) return undefined;
    if (process.env.DOCKER_HOST) return process.env.DOCKER_HOST;
    const result = await docker_(["context", "inspect", "--format", "{{.Endpoints.docker.Host}}"]);
    return result.exitCode === 0 && result.stdout ? result.stdout : undefined;
  })();
  return hostEndpoint;
}

/** Images this worker has already confirmed present, so the check runs once each. */
const presentImages = new Map<string, Promise<void>>();

export const docker = {
  /**
   * Ensures `image` is available locally.
   *
   * `docker pull` costs ~1.8s even when the image is already present, because it
   * still round-trips the registry to check the digest — several times the cost of
   * the container the test actually wants. `docker image inspect` answers the same
   * question locally in ~15ms, so pull only when it is genuinely missing, and
   * remember the answer for the rest of the worker's life.
   */
  async pull(image: string): Promise<void> {
    let pending = presentImages.get(image);
    if (!pending) {
      pending = (async () => {
        if ((await docker_(["image", "inspect", image])).exitCode === 0) return;
        const result = await docker_(["pull", image]);
        if (result.exitCode !== 0) throw new Error(`docker pull ${image} failed: ${result.stderr}`);
      })();
      presentImages.set(image, pending);
    }
    try {
      await pending;
    } catch (error) {
      // A failed attempt must not be cached as success for later tests.
      presentImages.delete(image);
      throw error;
    }
  },

  /**
   * Tags an existing local image under a new reference and removes the tag when
   * the test finishes. Used to place a stand-in image where lstk expects a real
   * emulator image, so start-path decisions can be asserted without pulling
   * gigabytes.
   */
  async tag(source: string, target: string): Promise<void> {
    const result = await docker_(["tag", source, target]);
    if (result.exitCode !== 0) throw new Error(`docker tag ${source} ${target} failed: ${result.stderr}`);
    onTestFinished(async () => {
      await docker_(["rmi", "--force", target]);
    });
  },

  async removeContainer(name: string): Promise<void> {
    await docker_(["rm", "--force", "--volumes", name]);
  },

  /** Container details, or null when no such container exists. */
  async inspectContainer(name: string): Promise<ContainerInfo | null> {
    const result = await docker_(["inspect", "--type", "container", name]);
    if (result.exitCode !== 0) return null;
    const [info] = JSON.parse(result.stdout) as ContainerInfo[];
    return info ?? null;
  },

  async containerIsRunning(name: string): Promise<boolean> {
    const info = await docker.inspectContainer(name);
    return info?.State.Running === true;
  },
};

export interface ContainerInfo {
  Name: string;
  State: { Running: boolean; ExitCode: number; Status: string };
  Config: { Image: string; Env: string[] };
  HostConfig: { Binds: string[] | null };
  NetworkSettings: {
    Ports: Record<string, Array<{ HostIp: string; HostPort: string }> | null>;
  };
}

/** Container names lstk uses; removed before and after any test that starts one. */
export const emulatorContainers = ["localstack-aws", "localstack-snowflake", "localstack-azure"];

/**
 * Serializes a whole describe block against every other worker on this machine
 * and clears leftover containers around it.
 *
 * lstk discovers a running emulator by (image, internal port), so two tests
 * starting a container at the same time would see each other's. This is the same
 * constraint the Go suite handles by not marking those tests parallel.
 */
export function useExclusiveEmulator(): void {
  const lockDir = path.join(os.tmpdir(), "lstk-e2e-emulator.lock");

  beforeAll(async () => {
    await acquire(lockDir);
    await Promise.all(emulatorContainers.map((name) => docker.removeContainer(name)));
  }, 300_000);

  afterAll(async () => {
    await Promise.all(emulatorContainers.map((name) => docker.removeContainer(name)));
    await rm(lockDir, { recursive: true, force: true });
  });
}

async function acquire(lockDir: string, timeoutMs = 240_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    try {
      await mkdir(lockDir);
      return;
    } catch {
      // A lock left behind by a killed run must not deadlock the next one.
      const age = await stat(lockDir)
        .then((s) => Date.now() - s.mtimeMs)
        .catch(() => 0);
      if (age > 600_000) {
        await rm(lockDir, { recursive: true, force: true });
        continue;
      }
      if (Date.now() > deadline) throw new Error(`timed out waiting for ${lockDir}`);
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }
}
