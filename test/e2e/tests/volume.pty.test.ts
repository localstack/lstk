import { execa } from "execa";
import { mkdir, mkdtemp, readdir, realpath, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { onTestFinished } from "vitest";
import { lstk, lstkPty, normalizeCliOutput, tempHome, type Home } from "../support/index.ts";

// Ported from test/integration/volume_test.go.
//
// None of this needs a running emulator — `lstk volume` only ever reads
// config and touches the on-disk volume directory it names — so unlike
// logs/reset there is no useExclusiveEmulator() here (the one subtest that
// does touch Docker uses a throwaway container, never the emulator's
// canonical name).
//
// Named .pty.test.ts because the confirm/cancel prompt is only reachable
// through a real terminal.
//
// Dropped: the two "emits telemetry" subtests (TestVolumePathCommand and
// TestVolumeClearCommand) assert only against a mock analytics server —
// mechanism, not something a user observes. Behaviour-wise they duplicate the
// plain-path/plain-clear cases already covered here.

async function tempVolumeDir(): Promise<string> {
  const dir = await mkdtemp(path.join(os.tmpdir(), "lstk-e2e-volume-"));
  onTestFinished(async () => {
    await rm(dir, { recursive: true, force: true });
  });
  return dir;
}

/** Escapes backslashes for a path embedded in a TOML quoted string (Windows). */
function tomlEscapePath(p: string): string {
  return p.replaceAll("\\", "\\\\");
}

/** Compares two paths after resolving symlinks, so a `/tmp` vs `/private/tmp`
 * style host quirk (macOS) never causes a false mismatch. */
async function expectSamePath(actual: string, expected: string): Promise<void> {
  const [a, e] = await Promise.all([
    realpath(path.resolve(actual)).catch(() => path.resolve(actual)),
    realpath(path.resolve(expected)).catch(() => path.resolve(expected)),
  ]);
  expect(a).toBe(e);
}

describe("lstk volume path", () => {
  test("prints the default volume path", async () => {
    const home = await tempHome();
    await home.writeConfig(`[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\n`);

    const run = await lstk(["volume", "path"], { home });

    expect(run).toSucceed();
    expect(run).toPrint(path.join("lstk", "volume", "localstack-aws"));
  });

  test("prints a custom volume path set via the legacy `volume` field", async () => {
    const customVolume = await tempVolumeDir();
    const home = await tempHome();
    await home.writeConfig(
      `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\nvolume = "${tomlEscapePath(customVolume)}"\n`,
    );

    const run = await lstk(["volume", "path"], { home });

    expect(run).toSucceed();
    await expectSamePath(run.stdout, customVolume);
  });

  test("follows a `volumes` entry targeting the persistence path", async () => {
    const persistDir = path.join(await tempVolumeDir(), "persist");
    const home = await tempHome();
    await home.writeConfig(
      [
        "[[containers]]",
        `type = "aws"`,
        `tag = "latest"`,
        `port = "4566"`,
        `volumes = ["${tomlEscapePath(persistDir)}:/var/lib/localstack", "/abs/init.sf.sql:/etc/localstack/init/ready.d/init.sf.sql"]`,
        "",
      ].join("\n"),
    );

    const run = await lstk(["volume", "path"], { home });

    expect(run).toSucceed();
    await expectSamePath(run.stdout, persistDir);
  });

  test("resolves a relative persistence source against the config file's directory", async () => {
    const home = await tempHome();
    const configFile = await home.writeConfig(
      `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\nvolumes = ["./persist:/var/lib/localstack"]\n`,
    );

    const run = await lstk(["volume", "path"], { home });

    expect(run).toSucceed();
    await expectSamePath(run.stdout, path.join(path.dirname(configFile), "persist"));
  });
});

describe("lstk volume clear", () => {
  test("clears the volume directory's contents with --force", async () => {
    const volumeDir = await tempVolumeDir();
    await mkdir(path.join(volumeDir, "cache", "certs"), { recursive: true });
    await writeFile(path.join(volumeDir, "cache", "certs", "cert.pem"), "fake cert");
    await writeFile(path.join(volumeDir, "cache", "machine.json"), "{}");

    const home = await tempHome();
    await home.writeConfig(
      `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\nvolume = "${tomlEscapePath(volumeDir)}"\n`,
    );

    const run = await lstk(["--non-interactive", "volume", "clear", "--force"], { home });

    expect(run).toSucceed();
    // volumeDir is a bare os.tmpdir() path, unrelated to the isolated home, so
    // it is masked explicitly here rather than via the home-masking built into
    // normalizeCliOutput; the size (11B for the two fixed-content files written
    // above) is stable and left in.
    expect(normalizeCliOutput(run.stdout, { extra: [[volumeDir, "<volume>"]] })).toPrintExactly(`
      LocalStack AWS Emulator: <volume> (11B)
      ✔︎ Volume data cleared
    `);

    // The directory itself survives; only its contents are gone.
    await expect(stat(volumeDir)).resolves.toBeDefined();
    expect(await readdir(volumeDir)).toEqual([]);
  });

  test("fails without --force in non-interactive mode", async () => {
    const home = await tempHome();
    await home.writeConfig(`[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\n`);

    const run = await lstk(["--non-interactive", "volume", "clear"], { home });

    expect(run).toExitWith(1);
    expect(run.stderr).toPrintExactly("Error: volume clear requires confirmation; use --force to skip in non-interactive mode");
  });

  test("succeeds even when the volume directory does not exist yet", async () => {
    const volumeDir = path.join(await tempVolumeDir(), "does-not-exist");
    const home = await tempHome();
    await home.writeConfig(
      `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\nvolume = "${tomlEscapePath(volumeDir)}"\n`,
    );

    const run = await lstk(["--non-interactive", "volume", "clear", "--force"], { home });

    expect(run).toSucceed();
    expect(normalizeCliOutput(run.stdout, { extra: [[volumeDir, "<volume>"]] })).toPrintExactly(`
      LocalStack AWS Emulator: <volume> (0B)
      ✔︎ Volume data cleared
    `);
  });

  test("filters by --type, failing for a type not in config and succeeding for one that is", async () => {
    const volumeDir = await tempVolumeDir();
    await writeFile(path.join(volumeDir, "data.json"), "{}");

    const home = await tempHome();
    await home.writeConfig(
      `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\nvolume = "${tomlEscapePath(volumeDir)}"\n`,
    );

    const wrongType = await lstk(
      ["--non-interactive", "volume", "clear", "--force", "--type", "snowflake"],
      { home },
    );
    expect(wrongType).toExitWith(1);
    expect(wrongType.stderr).toPrintExactly("Error: emulator type \"snowflake\" not found in config; available: [aws]");

    const rightType = await lstk(
      ["--non-interactive", "volume", "clear", "--force", "--type", "aws"],
      { home },
    );
    expect(rightType).toSucceed();
    expect(await readdir(volumeDir)).toEqual([]);
  });

  test.skipIf(process.platform !== "linux" || process.getuid?.() === 0)(
    "suggests sudo when the volume contains root-owned files",
    async () => {
      const volumeDir = await tempVolumeDir();

      // Simulate LocalStack creating files as root inside a bind-mounted volume.
      const setup = await execa(
        "docker",
        [
          "run",
          "--rm",
          "-v",
          `${volumeDir}:/vol`,
          "alpine",
          "sh",
          "-c",
          "mkdir /vol/cache && touch /vol/cache/cert.pem",
        ],
        { reject: false },
      );
      if (setup.exitCode !== 0) throw new Error(`docker setup failed: ${setup.stdout}\n${setup.stderr}`);
      onTestFinished(async () => {
        await execa("docker", ["run", "--rm", "-v", `${volumeDir}:/vol`, "alpine", "sh", "-c", "rm -rf /vol/cache"], {
          reject: false,
        });
      });

      const home = await tempHome();
      await home.writeConfig(
        `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\nvolume = "${tomlEscapePath(volumeDir)}"\n`,
      );

      const run = await lstk(["--non-interactive", "volume", "clear", "--force"], { home });

      if (run.exitCode === 0) {
        // Docker is configured with user namespace remapping; root-owned files
        // cleared without issue — nothing to assert.
        return;
      }
      expect(run).toExitWith(1);
      expect(run).toPrint("sudo");
    },
  );
});

describe("lstk volume clear (interactive)", () => {
  async function homeWithVolume(): Promise<{ home: Home; volumeDir: string }> {
    const volumeDir = await tempVolumeDir();
    await writeFile(path.join(volumeDir, "data.json"), "{}");
    const home = await tempHome();
    await home.writeConfig(
      `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\nvolume = "${tomlEscapePath(volumeDir)}"\n`,
    );
    return { home, volumeDir };
  }

  test("clears the volume when the user confirms with y", async () => {
    const { home, volumeDir } = await homeWithVolume();

    const term = lstkPty(["volume", "clear"], { home });
    await term.waitFor("Clear volume data?");
    term.type("y");

    expect(await term.exitCode()).toBe(0);
    expect(term.output()).toContain("Volume data cleared");
    expect(await readdir(volumeDir)).toEqual([]);
  });

  test("cancels and leaves the volume untouched when the user presses n", async () => {
    const { home, volumeDir } = await homeWithVolume();

    const term = lstkPty(["volume", "clear"], { home });
    await term.waitFor("Clear volume data?");
    term.type("n");

    expect(await term.exitCode()).toBe(0);
    expect(term.output()).toContain("Cancelled");
    expect(await readdir(volumeDir)).toEqual(["data.json"]);
  });
});
