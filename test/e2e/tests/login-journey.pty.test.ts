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
} from "../support/index.ts";

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
}

/** An isolated home wired to a mock platform, with the browser shimmed out. */
async function freshHome(
  keyring: "file" | "system" = "file",
  licenseToken?: string,
): Promise<Fixture> {
  const platform = await mockPlatform(licenseToken === undefined ? {} : { licenseToken });
  const browser = await fakeBrowser();
  const home = await tempHome({
    keyring,
    env: { LSTK_API_ENDPOINT: platform.url, LSTK_WEB_APP_URL: platform.url },
  });
  return { home, browser };
}

/** Drives the browser login flow to completion and returns the terminal output. */
async function login({ home, browser }: Fixture): Promise<string> {
  const term = lstkPty(["login"], { home, env: { PATH: browser.path } });
  await term.waitFor("Press any key when complete");
  term.press("enter");
  expect(await term.exitCode(), `login failed:\n${term.output()}`).toBe(0);
  return term.output();
}

async function assertLoginSticks(fixture: Fixture): Promise<void> {
  const { home } = fixture;

  // Before logging in, a non-interactive command cannot authenticate at all.
  const beforeLogin = await lstk(["start", "--non-interactive"], { home });
  expect(beforeLogin).toFail();
  expect(beforeLogin).toPrint("authentication required");

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
  const afterwards = await lstk(["start", "--non-interactive"], { home });
  expect(afterwards).toFail();
  expect(afterwards).toPrint("authentication required");
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
      await assertLoginSticks(await freshHome("system"));
    },
  );

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

  // The journey with a real license: the mock platform hands back the real token
  // as the login result, so `start` then brings up an actual emulator with nothing
  // in the environment to authenticate with.
  describe.skipIf(noDocker || !authToken())("through to a running emulator", () => {
    useExclusiveEmulator();

    test("starts the emulator using only the credential from login", async () => {
      const fixture = await freshHome("file", authToken());

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
