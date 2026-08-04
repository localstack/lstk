import { describe, expect, test } from "vitest";
import {
  authToken,
  browserCanBeFaked,
  docker,
  dockerIsAvailable,
  fakeBrowser,
  lstk,
  lstkPty,
  mockPlatform,
  realKeyringAllowed,
  requirement,
  tempHome,
  useExclusiveEmulator,
  type FakeBrowser,
  type Home,
  type MockPlatform,
} from "../support/index.ts";
import { defaultEmulatorName, privateEmulator, startStubEmulator } from "../support/emulator-stub.ts";

// Replaces what test/integration/{login,logout}_test.go assert about token
// storage. Where a credential is kept is an implementation detail, so nothing
// here inspects storage — the behaviour under test is that logging in sticks:
// lstk stops asking, later commands authenticate on their own, and logging out
// undoes exactly that.

const noDocker = requirement(
  "a container runtime",
  await dockerIsAvailable(),
  "Start a container runtime (Docker Desktop, Colima, Rancher Desktop, ...) so `docker info` succeeds.",
);
const noBrowserShim = requirement(
  "a shimmable browser opener",
  browserCanBeFaked,
  "Run on macOS or Linux: pkg/browser cannot be shimmed on Windows (rundll32).",
);

interface Fixture {
  home: Home;
  browser: FakeBrowser;
  platform: MockPlatform;
}

interface FreshHomeOptions {
  keyring?: "file" | "system";
  licenseToken?: string;
  /** false makes the platform never confirm the auth request, so login fails. */
  confirmed?: boolean;
}

/** An isolated home wired to a mock platform, with the browser shimmed out. */
async function freshHome(options: FreshHomeOptions = {}): Promise<Fixture> {
  const platform = await mockPlatform({
    ...(options.licenseToken === undefined ? {} : { licenseToken: options.licenseToken }),
    ...(options.confirmed === undefined ? {} : { confirmed: options.confirmed }),
  });
  const browser = await fakeBrowser();
  const home = await tempHome({
    keyring: options.keyring ?? "file",
    env: { LSTK_API_ENDPOINT: platform.url, LSTK_WEB_APP_URL: platform.url },
  });
  return { home, browser, platform };
}

/** Drives the browser login flow to completion and returns the terminal output. */
async function login({ home, browser }: Fixture): Promise<string> {
  const term = lstkPty(["login"], { home, env: { PATH: browser.path } });
  await term.waitFor("Press any key when complete");
  term.press("enter");
  expect(await term.exitCode(), `login failed:\n${term.output()}`).toBe(0);
  return term.output();
}

/**
 * Asserts that `start` cannot authenticate on its own.
 *
 * `start` checks the container runtime *before* auth, so on a machine without one it
 * fails with "Docker is not available" and says nothing about credentials — which
 * makes it useless as an auth probe there. Skipped rather than weakened, so the
 * assertion stays exact where it can run at all.
 */
async function expectCannotAuthenticate(home: Home, when: string): Promise<void> {
  if (noDocker) return;
  const run = await lstk(["start", "--non-interactive"], { home });
  expect(run, when).toFail();
  expect(run, when).toPrint("authentication required");
}

async function assertLoginSticks(fixture: Fixture): Promise<void> {
  const { home } = fixture;

  await expectCannotAuthenticate(home, "before logging in");

  expect(await login(fixture)).toContain("Login successful");

  // Asking again is answered from what login stored, with no prompt. On a
  // terminal, because `login` demands one before it checks whether there is
  // anything to do (cmd/login.go), so the scripted form cannot see this.
  const secondLogin = lstkPty(["login"], { home });
  await secondLogin.waitFor("You're already logged in");
  expect(await secondLogin.exitCode()).toBe(0);
  expect(
    secondLogin.output(),
    "a second login must not restart the browser flow",
  ).not.toContain("Opening browser to login");

  const loggedOut = await lstk(["logout"], { home });
  expect(loggedOut).toSucceed();
  expect(loggedOut).toPrint("Logged out successfully");

  // And the credential is gone for good, not just forgotten by `logout`.
  await expectCannotAuthenticate(home, "after logging out");
  expect(await lstk(["logout"], { home })).toPrint("Not currently logged in");
}

describe.skipIf(noBrowserShim)("the login journey", () => {
  test("logging in sticks: lstk stops asking, and logging out reverses it", async () => {
    await assertLoginSticks(await freshHome());
  });

  // The same journey against the real OS keyring instead of file storage. Same
  // assertions — this is not a storage test; it is the one run that would notice a
  // broken platform keyring adapter. Opt in with LSTK_E2E_REAL_KEYRING=1, or let CI
  // do it: there is one credential slot per machine and this overwrites, then
  // deletes, whatever is in it.
  test.skipIf(!realKeyringAllowed())(
    "logging in sticks when credentials go to the OS keyring",
    async () => {
      await assertLoginSticks(await freshHome({ keyring: "system" }));
    },
  );

  // A login the platform never confirms must leave nothing behind. The Go test this
  // replaces proved that by reading the keyring; the behavioural equivalent is that
  // both things a stored credential would change are still unchanged afterwards.
  test("a login the platform never confirms leaves lstk unauthenticated", async () => {
    const fixture = await freshHome({ confirmed: false });
    const { home, browser, platform } = fixture;

    const term = lstkPty(["login"], { home, env: { PATH: browser.path } });
    await term.waitFor("Press any key when complete");
    term.press("enter");

    expect(await term.exitCode(), term.output()).toBe(1);
    expect(term.output()).toContain("auth request not confirmed");
    expect(await browser.openedUrl(), "login still opens the auth URL").toBe(platform.authUrl);

    await expectCannotAuthenticate(home, "after a login that was never confirmed");

    // The second half of "nothing was stored": a retry runs the browser flow again
    // rather than short-circuiting the way a successful login makes it.
    const retry = lstkPty(["login"], { home, env: { PATH: browser.path } });
    await retry.waitFor("Press any key when complete");
    expect(
      retry.output(),
      "a failed login must leave nothing behind for the next one to find",
    ).not.toContain("You're already logged in");
    retry.press("enter");
    expect(await retry.exitCode()).toBe(1);
  });

  // LOCALSTACK_AUTH_TOKEN is the other way to be authenticated, and lstk treats it as
  // already-logged-in without touching stored credentials at all.
  test("LOCALSTACK_AUTH_TOKEN answers login without opening a browser", async () => {
    const browser = await fakeBrowser();
    const home = await tempHome({ env: { LOCALSTACK_AUTH_TOKEN: "test-env-token" } });

    const term = lstkPty(["login"], { home, env: { PATH: browser.path } });
    await term.waitFor("You're already logged in");

    expect(await term.exitCode()).toBe(0);
    expect(term.output()).not.toContain("Opening browser");
    expect(await browser.openedUrl(), "no browser tab for a token already in the env").toBe("");
  });

  test("logout says so when the token came from the environment, and leaves it working", async () => {
    const browser = await fakeBrowser();
    const home = await tempHome({ env: { LOCALSTACK_AUTH_TOKEN: "test-env-token" } });

    const loggedOut = await lstk(["logout"], { home });
    expect(loggedOut).toSucceed();
    expect(loggedOut).toPrint(
      "Authenticated via LOCALSTACK_AUTH_TOKEN environment variable; unset it to log out",
    );

    // Not just a message: logout cannot clear an env var, and the credential is
    // still in effect afterwards.
    const term = lstkPty(["login"], { home, env: { PATH: browser.path } });
    await term.waitFor("You're already logged in");
    expect(await term.exitCode()).toBe(0);
  });

  // `logout` ends the session but leaves containers up, so it names what it left
  // behind. Only when it actually logged someone out: the not-logged-in path returns
  // before the check (internal/auth/auth.go), so these all log in first.
  describe.skipIf(noDocker)("logout reports the emulators it leaves running", () => {
    test("names the one it leaves running", async () => {
      const fixture = await freshHome();
      const emulator = privateEmulator("aws");
      await fixture.home.writeConfig(emulator.config);
      await startStubEmulator(emulator.name);

      await login(fixture);

      const loggedOut = await lstk(["logout"], { home: fixture.home });
      expect(loggedOut).toSucceed();
      expect(loggedOut).toPrint("LocalStack AWS Emulator is still running in the background");
    });

    // Two enabled [[containers]] blocks are rejected by `start`, but every recovery
    // command still has to enumerate them — so logout names both, in config order.
    test("names every one it leaves running", async () => {
      const fixture = await freshHome();
      const aws = privateEmulator("aws");
      const snowflake = privateEmulator("snowflake", { port: "4567" });
      await fixture.home.writeConfig(aws.config + snowflake.config);
      await startStubEmulator(aws.name);
      await startStubEmulator(snowflake.name);

      await login(fixture);

      const loggedOut = await lstk(["logout"], { home: fixture.home });
      expect(loggedOut).toSucceed();
      expect(loggedOut).toPrint(
        "LocalStack AWS Emulator, LocalStack Snowflake Emulator are still running in the background",
      );
    });

    // Holds the lock for the same reason as the start test below: a container built
    // from a real `localstack/*` image reference is visible to every other test's
    // "is an emulator running" check.
    describe("with a container built from a real emulator image reference", () => {
      useExclusiveEmulator();

      test("does not report a running emulator of a type the config did not select", async () => {
        // A real emulator image reference, so a snowflake-typed config reaching
        // the image fallback still finds nothing to match: the fallback is
        // scoped to the repos of the configured type.
        const awsImage = "localstack/localstack-pro:logout-journey-test";
        await docker.pull("alpine:latest");
        await docker.tag("alpine:latest", awsImage);
        await startStubEmulator(defaultEmulatorName, { image: awsImage });

        // Control first, so the assertion below cannot pass merely because
        // nothing was discoverable. It resolves by container name, which is the
        // deterministic half of discovery — the image/port fallback depends on a
        // published port, and port 4566 is shared machine state even under this
        // lock.
        const awsFixture = await freshHome();
        await login(awsFixture);
        expect(await lstk(["logout"], { home: awsFixture.home })).toPrint(
          "LocalStack AWS Emulator is still running in the background",
        );

        // Same container, only the configured type differs.
        const fixture = await freshHome();
        await fixture.home.writeConfig(
          `[[containers]]\ntype = "snowflake"\ntag = "latest"\nport = "4566"\n`,
        );
        await login(fixture);

        const loggedOut = await lstk(["logout"], { home: fixture.home });
        expect(loggedOut).toSucceed();
        expect(
          loggedOut,
          "an AWS container must not satisfy a snowflake-targeted config",
        ).not.toPrint("still running");
      });
    });
  });

  // Holds the exclusive lock: this is one of the few tests that starts a container
  // from a real `localstack/*` image reference. Emulator discovery falls back to
  // matching any known localstack image exposing port 4566 when the configured name
  // is absent (internal/container/running.go), and the internal port is always 4566
  // whatever the config says — so such a container is visible to every other test's
  // "is an emulator running" check, private tag or not.
  describe("with a container built from a real emulator image reference", () => {
    useExclusiveEmulator();

    test("a later start authenticates on its own, with no token in the environment", async () => {
      const fixture = await freshHome();
      await login(fixture);

      // A pinned tag whose image is present locally skips the pull and the license
      // pre-flight, so the run reaches the container without needing a real license —
      // far enough to show that auth was satisfied from what login stored.
      const pinnedTag = "login-journey-test";
      const pinnedImage = `localstack/localstack-pro:${pinnedTag}`;
      if (!noDocker) {
        await docker.pull("alpine:latest");
        await docker.tag("alpine:latest", pinnedImage);
      }
      await fixture.home.writeConfig(
        `[[containers]]\ntype = "aws"\ntag = "${pinnedTag}"\nport = "4598"\n`,
      );

      const run = await lstk(["start", "--non-interactive"], { home: fixture.home });

      expect(run, "start must not ask for credentials again").not.toPrint(
        "authentication required",
      );
      if (!noDocker) {
        await docker.removeContainer(`localstack-aws-${pinnedTag}`);
      }
    });
  });

  // The journey with a real license: the mock platform hands back the real token
  // as the login result, so `start` then brings up an actual emulator with nothing
  // in the environment to authenticate with.
  describe.skipIf(noDocker || !authToken())("through to a running emulator", () => {
    useExclusiveEmulator();

    test("starts the emulator using only the credential from login", async () => {
      const fixture = await freshHome({ licenseToken: authToken() });

      await login(fixture);
      const run = await lstk(["start", "--non-interactive"], { home: fixture.home });

      expect(run).toSucceed();
      const status = await lstk(["status"], { home: fixture.home });
      expect(status, "the emulator a user logged in for is now usable").toPrint(
        "is running",
      );
    });
  });
});
