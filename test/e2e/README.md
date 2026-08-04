# lstk e2e tests (TypeScript / Vitest)

End-to-end tests that drive the **built binary** (`bin/lstk`) as a user would: no lstk
source code is imported, nothing is stubbed inside the process.

This suite owns the CLI boundary for the areas it covers — the proxy commands, `--json`,
exit codes, lifecycle, `logs`, `volume`, config, completion, `docs`, login and the TUI —
and the Go tests for those areas have been removed. The Go suite in `test/integration/`
keeps everything else. Which suite owns what, and why each remaining Go test could not
move, is tracked in [PORTING.md](PORTING.md).

## Toolchain

**Node >= 26** and **pnpm**. Node version is pinned in [.node-version](.node-version)
(`fnm use`, `nvm use`, and `actions/setup-node` all read it).

Typechecking is **TypeScript 7** (the native compiler). The suite is written in
**type-erasable TypeScript only** — `erasableSyntaxOnly` is on, so no enums, namespaces,
parameter properties or import aliases, and imports name the real file (`./lstk.ts`, not
`./lstk.js`). Nothing here needs a transform beyond deleting the types, which is exactly
what Node itself does natively.

**Every `@types/*` dependency must be listed in `tsconfig.json`'s `types` array.**
Otherwise global types arrive by accident: `@types/node`'s globals currently reach this
project transitively through execa and vitest, so removing `"node"` from `types`
typechecks fine today and breaks the day a dependency stops referencing it.

pnpm blocks dependency install scripts by default; the two this suite needs are
allowed in [pnpm-workspace.yaml](pnpm-workspace.yaml) (pnpm 11 reads settings from
there, not from `package.json`).

## Running

```bash
make test-e2e
```

That builds `bin/lstk` first, installs deps if missing, then runs Vitest. To iterate
on one file:

```bash
cd test/e2e && pnpm exec vitest tests/emulator-type.test.ts
```

Typecheck (also a CI step, since `erasableSyntaxOnly` violations only surface here):

```bash
cd test/e2e && pnpm exec tsc --noEmit
```

Filter by test name across the suite with `make test-e2e RUN="switches an existing config"`.

### Prerequisites

The PTY binding is **required** — see "Terminal tests" below. Everything else skips
when absent, so a contributor missing a piece can still run the rest:

| Prerequisite | Skipped when absent |
| --- | --- |
| A container runtime (`docker info` succeeds) | container tests |
| `LOCALSTACK_AUTH_TOKEN` | tests needing a real license |
| A shimmable browser opener (not Windows) | browser login-flow tests |

That leniency is also how coverage erodes unnoticed, so **`LSTK_E2E_REQUIRE_ALL=1`
turns a missing prerequisite into a collection-time failure** naming the fix. CI sets
it on the Linux leg, which has everything; macOS runners have no container runtime and
Windows can run neither Linux containers nor a shimmed browser, so those stay lenient.

Declare a new prerequisite through `requirement()` ([requirements.ts](support/requirements.ts))
rather than an ad-hoc boolean, or strict mode cannot see it.

## How a test reads

```ts
test("switches an existing config in place, preserving comments", async () => {
  const home = await tempHome(noDaemon);
  await home.writeConfig(`[[containers]]\ntype = "aws"     # keep me\nport = "4566"\n`);

  const run = await lstk(["start", "--type", "azure", "--non-interactive"], { home });

  expect(run).toPrint("Switched configured emulator to Azure");
  expect(await home.readConfig()).toContain(`type = "azure"`);
  expect(await home.readConfig(), "the rewrite is surgical").toContain("# keep me");
});
```

Three blocks, always in this order: arrange the isolated home, run the binary once,
assert. Everything else lives in `support/`.

## The DSL (`support/`)

| Module | What it gives a test |
| --- | --- |
| `lstk.ts` | `lstk(args, { home, env, cwd, stdin })` → `{ stdout, stderr, exitCode, command }`. Never throws on non-zero exit. |
| `home.ts` | `tempHome()` → an isolated HOME with its own config dir, cache dir and file keyring; `home.configPath()` asks the binary itself; `writeConfig` / `readConfig` / `configExists`. Cleans up after the test. |
| `pty.ts` | `lstkPty(args, { home })` → `waitFor` / `expectNever` / `press("enter")` / `exitCode()`, with ANSI stripped so assertions match what a human sees. |
| `docker.ts` | `dockerIsAvailable()`, `docker.pull/tag/inspectContainer/containerIsRunning`, and `useExclusiveEmulator()` for describe blocks that start a container. |
| `license.ts` | `mockLicenseServer("grants" \| "rejects" \| { body })` — a stand-in license API, closed automatically. |
| `platform.ts` | `mockPlatform()` — the full browser login flow, so a test can reach a logged-in state via `lstk login`; `fakeBrowser()` shims `open`/`xdg-open` on PATH and records the URL. |
| `envelope.ts` | `parseEnvelope(stdout)` for the `--json` contract. |
| `matchers.ts` | `toSucceed()`, `toFail()`, `toExitWith(n)`, `toPrint(text \| regex)`; failures print the invocation plus both streams. |

Two deliberate choices worth knowing:

- **Docker is driven through the `docker` CLI**, not an API client — it resolves
  `DOCKER_HOST`, contexts and non-Docker runtimes exactly as a user's shell does, and
  keeps the suite free of native dependencies.
- **Every isolated home gets `DOCKER_HOST` set** to the harness's active context
  endpoint. Most runtimes keep their socket under the real home
  (`~/.docker/run/docker.sock`, `~/.colima/default/docker.sock`), which the binary can
  no longer find once `HOME` is a temp dir. It is a no-op on CI, where
  `/var/run/docker.sock` is already correct.

## Terminal tests

Roughly half the suite drives lstk on a PTY, so the binding
(`@homebridge/node-pty-prebuilt-multiarch`) is a **required** dependency, imported
statically in [pty.ts](support/pty.ts) and checked once in
[global-setup.ts](support/global-setup.ts). It was an `optionalDependency` at first;
that was a mistake — npm blocked its build script, the binding never loaded, every
terminal test skipped, and the run stayed green.

It is a native module, but it does not build from source here: `node-pty` carries
Node-API prebuilds for macOS, Windows and Linux (x64 and arm64) inside its npm tarball,
so nothing is fetched or compiled at install and a Node major bump does not strand it.
Its install script still has to be allowed in
[pnpm-workspace.yaml](pnpm-workspace.yaml), or no binary is staged at all. When the
binding cannot load, the run fails with the likely causes spelled out instead of
skipping.

### The version is pinned exactly, on purpose

`node-pty@1.2.0-beta.14`, not `^1.1.0`. Two reasons, both measured:

- **1.1.0 (current stable) is broken on macOS.** Its tarball ships
  `prebuilds/darwin-*/spawn-helper` with mode `0644`, and neither its `install` nor its
  `postinstall` script fixes that, so every spawn dies with `posix_spawnp failed`. Not a
  pnpm artifact — plain `npm install` reproduces it. `chmod +x` on the helper is enough
  to fix it, which is what the beta does at pack time (`0755`).
- **1.1.0 ships no Linux prebuilds**, so Linux always runs node-gyp — fine on a CI
  runner with python3 and a compiler, fatal on `node:26-slim` or `node:26-alpine`. The
  beta adds `linux-x64` and `linux-arm64`.

Loosening the pin therefore needs a check, not a hope: install it and actually spawn a
PTY on macOS and on a toolchain-free Linux image. Alpine/musl is the one gap — the beta's
Linux prebuilds are glibc-only, so a musl image would have to compile (verified:
`node:26-alpine` fails to load the prebuilt `pty.node`). Nothing in CI runs on musl.

### Windows

node-pty drives ConPTY, so terminal tests are not gated to Unix — unlike the Go suite,
where `creack/pty` has no Windows support and every TUI test skips. Whether Bubble Tea
renders identically over ConPTY is unproven; the Windows CI leg is what answers it, and
[tui-runtime-error.pty.test.ts](tests/tui-runtime-error.pty.test.ts) is the probe (no
Docker, no browser, so it runs everywhere). `stripAnsi` carries its own tests because
ConPTY emits more escape sequences than a Unix PTY and a missed one would silently make
`waitFor` blind.

What still cannot run on Windows is the **browser login flow**: `pkg/browser` invokes
`rundll32 url.dll,FileProtocolHandler` there instead of a shimmable `open`/`xdg-open`
script (`browserCanBeFaked`). Since login is the only way to reach a logged-in state
without touching the store, real-keyring coverage on Windows stays out of reach until
either the browser opener or the keyring identity is overridable.

## Assert behaviour, not mechanism

Tests here describe what a user can observe from the CLI. Anything the CLI does not
expose — where a credential is stored, which internal type handled a call — is out of
scope, because encoding it makes the test a second copy of the implementation.

Three rules follow from that:

1. **Assert through the CLI where the CLI can answer.** "The emulator started" is
   `lstk status` reporting `is running` with an endpoint, not a container of a
   particular name existing in Docker.
2. **A user-facing artifact is fair game; internal storage is not.** The config file
   counts — it is documented, users edit it, `lstk config path` prints it, and
   `--type` is *defined* as rewriting it. The keyring does not.
3. **When the CLI's own message is the only observable, say so in the test.** One test
   does this ([start-local-image](tests/start-local-image.test.ts)) and the comment
   there explains why; treat it as the exception, not a licence.

Harness self-tests (tests of this suite's own helpers, not of lstk) live under
[tests/harness/](tests/harness) so they are never mistaken for product behaviour.

## Exact output: `toPrintExactly`, never a snapshot

lstk's output is a contract we control, so assert it whole and assert it deliberately:

```ts
expect(run).toExitWith(1);
expect(run.stdout).toPrintExactly(`
  Error: LocalStack AWS Emulator is not running
    ==> Start LocalStack: lstk
    ==> See help: lstk -h
`);
```

`toPrintExactly` (in [support/matchers.ts](support/matchers.ts)) dedents the expected
block — leading/trailing blank lines dropped, common indentation stripped — so it can sit
at the indentation of the surrounding code while still matching byte for byte. Nesting
*within* the block survives, which is what makes those `==>` lines assertable.

**Inline snapshots are not used here**, deliberately:

- A snapshot is *recorded*; a literal is *authored*. Only one of them reads as a promise
  about what users see, and only one cannot be rewritten by a stray `vitest -u`.
- `toMatchInlineSnapshot` throws inside `test.each` ("InlineSnapshot cannot be used
  inside of test.each"). That cost this suite a 4× file expansion before the matcher
  existed — 14 cases as 14 near-identical tests instead of two tables.

The rest of the rules stand whatever the mechanism:

- **Exact assertions are for output only.** Structured data — argv, environment, parsed
  JSON envelopes — gets `toEqual` / `toMatchObject`.
- **Only assert output that is identical on every machine and every run.** Reject a temp
  path, port, duration, container ID, version, or — the subtle one — text that adapts to
  the host, such as the Docker-unreachable message, whose suggested start commands depend
  on which runtimes are installed. Fall back to `toPrint` and say why in a comment, as
  [aws-proxy.test.ts](tests/aws-proxy.test.ts) does.
- **Mask only what is incidental.** `normalizeCliOutput()` in
  [support/cli-output.ts](support/cli-output.ts) masks a test's temp home. If the value
  itself is the point — a resolved config path, a specific port — assert it instead.
- **Keep the promise legible.** Exact text says what the output is, not why it matters.
  Where the intent is load-bearing — an error must offer a way forward — say so in a
  comment.

Prefer an exact assertion to an absence check: `not.toPrint("lstk setup aws")` became
`toPrintExactly("")`, which also catches any *other* unwanted output.

Credentials are the worked example. Nothing reads or writes token storage; the
journey in [login-journey.pty.test.ts](tests/login-journey.pty.test.ts) is:

1. `start` before logging in → fails, "authentication required"
2. `lstk login` → "Login successful"
3. `lstk login` again → "You're already logged in", browser flow not restarted
4. `start` with **no** token in the environment → gets past auth on its own
5. `logout` → "Logged out successfully", and `start` fails again as in (1)

That is strictly stronger than reading the keyring: it shows the credential is
*usable*, not merely present, and it holds for whichever backend the binary picked.

`tempHome()` defaults to `LSTK_KEYRING=file` so a test never touches machine-wide
state. `tempHome({ keyring: "system" })` runs the same journey against the real OS
keyring — the one run that would notice a broken platform adapter — and is skipped
unless `LSTK_E2E_REAL_KEYRING=1` or `CI=true`, since service and account are hardcoded
in `internal/auth/token_storage.go`: one slot per machine, which the journey
overwrites and then deletes.

## Parallelism

Test files run in parallel workers. Tests that start an emulator call
`useExclusiveEmulator()`, which takes a machine-wide lock and clears leftover
containers before and after — lstk discovers a running emulator by (image, internal
port), so two of them at once would see each other. Same constraint the Go suite
handles by not marking those tests parallel.

## CI

The `test-e2e` job in `.github/workflows/ci.yml` runs on ubuntu / macOS / windows,
writes JUnit XML (`CREATE_JUNIT_REPORT=1`), uploads it, and renders it through
`dorny/test-reporter` — same reporting as the Go suite. It is **not** in the release
job's `needs:` yet; it has to be promoted to a required check, since it is now the only
suite covering the CLI boundary for the areas it owns.

Vitest shards natively if the suite grows enough to need it: set `SHARD_INDEX` /
`SHARD_TOTAL` (`scripts/test-e2e.sh` forwards them to `--shard`).

## Known costs

- **Second toolchain.** Node >= 26, pnpm and a lockfile in a Go repo; contributors
  need both, and CI grows a Node setup step per leg.
- **The PTY binding is a required native module** pinned to an exact prerelease. Prebuilt
  for every platform CI runs on, with no compile and no install-time download — but
  bumping it needs a real check, not a version bump. See "Terminal tests".
- **Install-script approvals are a maintenance step.** Adding a dependency that needs
  one means updating `pnpm-workspace.yaml`, or the install fails closed.
- **No keyring library.** Reading the store directly from Node is possible but needs
  per-platform glue that mirrors `go-keyring` exactly (macOS: `security` plus a
  `go-keyring-base64:` value prefix; Windows: no read-capable CLI, so PowerShell
  P/Invoke on `CredReadW` with TargetName `<service>:<account>`; Linux: `secret-tool`
  with `{service, username}` attributes and a live Secret Service). Off-the-shelf
  bindings do not interoperate — `@napi-rs/keyring` wraps keyring-rs, whose Windows
  target naming differs, and keytar is archived. The CLI-driven approach above avoids
  all of it.
