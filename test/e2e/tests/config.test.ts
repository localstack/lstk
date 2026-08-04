import { mkdir, realpath, writeFile } from "node:fs/promises";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { lstk, normalizeCliOutput, tempHome, unreachableDockerHost } from "../support/index.ts";
import { osConfigDir, xdgConfigDir } from "../support/os-config-dir.ts";
import { canonicalPath } from "../support/paths.ts";

// Ported from test/integration/config_test.go.
//
// `lstk config path` is side-effect-free path resolution only (cmd/config.go's
// RunE never opens the file — with --config it just echoes the flag value
// back). So the "config path" cases here cover resolution/precedence, and the
// cases that need real parsing behaviour (unknown fields tolerated, a missing
// required field rejected) instead run `lstk logout`, which calls
// config.Get() early and — with no stored session and a file keyring — still
// succeeds without Docker or an auth token. That is a deliberate improvement
// over the Go originals (TestConfigWithUnknownFieldsIsAccepted and
// TestConfigWithMissingOptionalTagSucceeds), which used `config path` too and
// so never actually exercised parsing; see the report for details.
//
// TestConfigFlagEnvVarsPassedToContainer is not ported: it requires Docker and
// asserts on a container's inspected env vars, which is internal mechanism,
// not something a user observes from the CLI.

const noDaemon = { env: { DOCKER_HOST: unreachableDockerHost } };
const validConfig = `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\n`;

describe("lstk config path", () => {
  test("--config overrides the resolved config path", async () => {
    const home = await tempHome();
    const customConfig = path.join(home.path, "custom.toml");
    await writeFile(customConfig, validConfig);

    const run = await lstk(["--config", customConfig, "config", "path"], { home });

    expect(run).toSucceed();
    expect(run.stdout).toBe(customConfig);
  });

  test("a project-local .lstk/config.toml wins over the XDG and OS-default locations", async () => {
    const home = await tempHome();
    const workDir = path.join(home.path, "workdir");
    const xdgOverride = path.join(home.path, "xdg-config-home");
    await mkdir(workDir, { recursive: true });

    const localConfig = path.join(workDir, ".lstk", "config.toml");
    const xdgTierConfig = path.join(xdgConfigDir(home.path), "config.toml");
    const osDefaultConfig = path.join(osConfigDir(home.path, xdgOverride), "config.toml");
    for (const file of [localConfig, xdgTierConfig, osDefaultConfig]) {
      await mkdir(path.dirname(file), { recursive: true });
      await writeFile(file, validConfig);
    }

    const run = await lstk(["config", "path"], {
      home,
      cwd: workDir,
      env: { XDG_CONFIG_HOME: xdgOverride },
    });

    expect(run).toSucceed();
    // The local tier is resolved through the process's cwd, so on macOS the
    // binary's os.Getwd() sees the real, symlink-resolved path (/private/var/...)
    // even though we chdir'd via the unresolved /var/... alias; resolve on our
    // side too before comparing, same as the Go suite's own normalizedPath helper.
    expect(await canonicalPath(run.stdout)).toBe(await canonicalPath(localConfig));
  });

  test("the $HOME/.config/lstk location wins over the OS-default location", async () => {
    const home = await tempHome();
    const xdgOverride = path.join(home.path, "xdg-config-home");

    const xdgTierConfig = path.join(xdgConfigDir(home.path), "config.toml");
    const osDefaultConfig = path.join(osConfigDir(home.path, xdgOverride), "config.toml");
    for (const file of [xdgTierConfig, osDefaultConfig]) {
      await mkdir(path.dirname(file), { recursive: true });
      await writeFile(file, validConfig);
    }

    const run = await lstk(["config", "path"], { home, env: { XDG_CONFIG_HOME: xdgOverride } });

    expect(run).toSucceed();
    expect(run.stdout).toBe(xdgTierConfig);
  });

  test("prints the OS-default location and creates nothing when no config exists yet", async () => {
    const home = await tempHome({ xdgConfigDir: false });

    const run = await lstk(["config", "path"], { home });

    expect(run).toSucceed();
    expect(run.stdout).toBe(path.join(osConfigDir(home.path), "config.toml"));
    expect(await home.configExists()).toBe(false);
  });
});

describe("lstk config parsing", () => {
  test("tolerates unknown fields for forward compatibility", async () => {
    const home = await tempHome();
    const configFile = path.join(home.path, "config.toml");
    await writeFile(
      configFile,
      [
        `unknown_top_level = "should be ignored"`,
        "",
        "[[containers]]",
        `type = "aws"`,
        `tag = "latest"`,
        `port = "4566"`,
        `future_field = "should be ignored"`,
        "",
      ].join("\n"),
    );

    const run = await lstk(["--config", configFile, "logout"], { home, ...noDaemon });

    expect(run).toSucceed();
  });

  test("succeeds when the optional tag is missing", async () => {
    const home = await tempHome();
    const configFile = path.join(home.path, "config.toml");
    await writeFile(configFile, `[[containers]]\ntype = "aws"\nport = "4566"\n`);

    const run = await lstk(["--config", configFile, "logout"], { home, ...noDaemon });

    expect(run).toSucceed();
  });

  test("fails with a helpful message when the required port is missing", async () => {
    const home = await tempHome();
    const configFile = path.join(home.path, "config.toml");
    await writeFile(configFile, `[[containers]]\ntype = "aws"\ntag = "latest"\n`);

    const run = await lstk(["--config", configFile, "stop", "--non-interactive"], { home, ...noDaemon });

    expect(run).toFail();
    expect(run.stderr).toPrintExactly("Error: failed to get config: invalid container config: port is required for aws emulator");
  });

  // Rejected at config load, so this needs neither a daemon nor a token.
  test("fails with a helpful message when container_name is not a legal Docker name", async () => {
    const home = await tempHome();
    const configFile = path.join(home.path, "config.toml");
    await writeFile(configFile, `[[containers]]\ntype = "aws"\ntag = "latest"\nport = "4566"\ncontainer_name = "my emulator"\n`);

    const run = await lstk(["--config", configFile, "stop", "--non-interactive"], { home, ...noDaemon });

    expect(run).toFail();
    expect(run.stderr).toPrintExactly(
      `Error: failed to get config: invalid container config: invalid container name "my emulator": must start with a letter or digit and use only letters, digits, dots, hyphens, and underscores`,
    );
  });

  test("a legacy config.yaml gives a helpful TOML migration error", async () => {
    const home = await tempHome();
    const legacyConfigDir = path.join(home.path, ".config", "lstk");
    await mkdir(legacyConfigDir, { recursive: true });
    await writeFile(path.join(legacyConfigDir, "config.yaml"), "emulators:\n  - type: aws\n    port: 4566\n");

    const run = await lstk(["logout", "--non-interactive"], { home });

    expect(run).toFail();
    expect(normalizeCliOutput(run.stderr, { home, posixSeparators: true })).toPrintExactly("Error: <home>/.config/lstk/config.yaml is from an old lstk version; lstk now uses TOML format — remove it or replace it with a config.toml file");
  });
});

describe("lstk start", () => {
  test("rejects a config with more than one [[containers]] block", async () => {
    const home = await tempHome();
    const configFile = path.join(home.path, "config.toml");
    await writeFile(
      configFile,
      ['[[containers]]', `type = "aws"`, `port = "4566"`, "", '[[containers]]', `type = "snowflake"`, `port = "4567"`, ""].join(
        "\n",
      ),
    );

    // The guard runs at the very top of container.Start, before any Docker
    // health check, auth, or image pull, so this fails fast without a daemon.
    const run = await lstk(["--config", configFile, "start", "--non-interactive"], { home, ...noDaemon });

    expect(run).toFail();
    expect(run.stdout).toPrintExactly(`
      Error: Unsupported configuration
        found 2 [[containers]] blocks in your config, but only one is supported at a time
        ==> Edit your config file so only one [[containers]] block is enabled: lstk config path
    `);
  });
});
