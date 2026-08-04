# Porting status: Go integration suite → TypeScript e2e suite

**Intent:** this suite owns the CLI boundary for the areas listed under "Covered". Go
**unit** tests (`cmd/`, `internal/`) stay as they are — they are the right tool for logic
with no CLI surface. `test/integration` keeps everything under "Still owned by Go" and
does not go away wholesale; it shrinks as areas move across.

**Status: 175 tests across 23 files** (167 pass, 8 skip on this machine — the skips are
the whole of `start.test.ts`, which needs an auth token). Full-suite wall clock ≈ 33s.

The Go integration suite has been trimmed accordingly: against `main` it goes from **419
→ 302** test functions across **54 → 44** files, 14,552 → 11,598 lines.

## What "ported" means here

Not a line-by-line translation. Many Go integration tests assert mechanism rather
than behaviour, and those were deliberately **not** carried across:

- **Telemetry assertions** (`assertCommandTelemetry`, `mockAnalyticsServer`) — dropped
  throughout. What lstk sends to analytics is not something a CLI user observes. Per-command
  telemetry stays in Go, consolidated into one table in
  `test/integration/command_telemetry_test.go` rather than scattered across the tests whose
  behavioural halves moved here.
- **Container introspection** (`docker inspect` of `Config.Env`, `HostConfig.Binds`)
  — replaced by the CLI-observable equivalent where one exists, e.g. `restart
  --persist` is asserted through the `• Persistence: Enabled` line rather than the
  container's environment.
- **Token storage** — five keyring assertions across `login_test.go` and
  `logout_test.go` became one behavioural journey (see the README).

Two Go tests turned out to be **vacuous** and were re-targeted rather than copied:
`TestConfigWithUnknownFieldsIsAccepted` and `TestConfigWithMissingOptionalTagSucceeds`
assert config acceptance through `lstk config path`, which never parses the file (see
`cmd/config.go`) — they pass against a nonexistent path. The ports use `lstk logout`, which
really calls `config.Get()`.

## Covered

| Area | Vitest files | Tests |
| --- | --- | --- |
| Proxy commands (`aws`, `terraform`) | `aws-proxy`, `terraform-proxy` | 41 |
| `--json` envelope, exit codes, `--non-interactive` | `json-envelope`(+`.pty`), `json-flag`, `exit-codes`, `non-interactive.pty` | 42 |
| Lifecycle (`stop`, `restart`, `status`, `reset`) | `stop-restart`, `status`, `reset.pty` | 22 |
| `logs`, `volume` | `logs.pty`, `volume.pty` | 20 |
| Config, completion, docs | `config`, `completion`, `docs` | 21 |
| Start paths, emulator selection, login journey, TUI | `start`, `start-local-image`, `emulator-select.pty`, `emulator-type`, `login-journey.pty`, `tui-runtime-error.pty` | 17 |
| Harness self-tests (not product behaviour) | `harness/strip-ansi`, `harness/print-exactly` | 12 |

### What that removed from the Go suite

Deleted outright: `json_envelope`, `exit_code`, `non_interactive`, `completion`, `docs`,
`terraform_cmd`, `logs`, `reset`, `volume`, `stop`, `restart`.

`stop_test.go` and `restart_test.go` each gained an `LSTK_ENDPOINT_URL` rejection test on
`main` after this port was written. Those two moved into `endpoint_url_test.go` alongside
their 27 siblings rather than being deleted with the rest of their files.

`config_test.go` likewise gained `TestConfigWithInvalidContainerNameFails` on `main`
(custom `container_name`). That one is pure CLI output rejected at config load — no
daemon, no token — so it was ported to `tests/config.test.ts` next to its `port is
required` sibling rather than kept. Its companion `TestStartCommandUsesCustomContainerName`
stays in Go: it needs a real start plus a container inspect.

Trimmed, with the reason each remainder stayed:

| Go file | Kept | Why it could not move |
| --- | --- | --- |
| `json_flag` | 2 of 7 | Both are table-driven proxy tests covering `az`, which TypeScript cannot reach without a completed `lstk setup azure` |
| `aws_cmd` | 3 of 18 | Spinner timing under a PTY |
| `status` | 2 of 7 | `ShowsResourcesWhenRunning` needs an AWS SDK client; `WorksWithNonDefaultPort` binds the `127.0.0.2` loopback alias, which Docker Desktop rejects and a native Linux daemon accepts |
| `config` | 1 of 12 | `TestConfigFlagEnvVarsPassedToContainer` inspects the container's environment |
| `emulator_type` | 7 of 10 | |
| `emulator_select` | 7 of 9 | |
| `start` | 33 of 35 | |
| `login` | 3 of 4 | |
| `logout` | 4 of 6 | |

## Still owned by Go

| Area | Go files | Cases | Would need |
| --- | --- | --- | --- |
| Snapshots | `snapshot_*_test.go`, `start_snapshot_test.go` | 78 | Mock cloud/S3 remotes; AWS SDK assertions against a live emulator |
| IaC end-to-end | `terraform_e2e`, `terraform_s3backend_e2e`, `cdk_*`, `sam_*` | 46 | Real terraform/cdk/sam installs (the `_cmd` half of terraform is done) |
| `start` remainder | `start_test.go`, `docker_unhealthy`, `docker_windows` | 40 | Never-healthy image via `docker commit`; bind/port introspection |
| Trimmed leftovers | `emulator_type`, `emulator_select`, `logout`, `login`, `aws_cmd`, `status`, `json_flag`, `config` | 29 | See the table above — each has its own blocker |
| `az` proxy, `setup azure`, `awsconfig` | `az_*`, `setup_azure`, `awsconfig` | 22 | Isolated `~/.azure` assertions; `setup azure` completion marker |
| `--endpoint-url` / `LSTK_ENDPOINT_URL` | `endpoint_url`, `endpoint_url_https` | 27 | Landed after the port. It spans commands this suite owns (`logs`, `volume`, `stop`, `restart`, `start`, `status`, `aws`) but is one feature with one design doc, and several cases assert container state through the Docker SDK — splitting it across two suites would cost more than it buys. Port it as a unit or not at all. |
| Extensions, signal forwarding | `extension`, `signal_forwarding` | 22 | Reference extension build; process-group signalling |
| Update & install | `update`, `multiple_installs`, `version_resolution` | 16 | Mock GitHub releases API; fake Homebrew/npm layouts |
| Telemetry, license, logging | `telemetry`, `license`, `logging`, `command_telemetry` | 15 | Mechanism by design — a mock analytics server and a mock license API, neither of which is user-observable |

Two individually dropped cases worth revisiting:

- `TestStatusCommandShowsResourcesWhenRunning` — needs an AWS SDK client to create
  S3/SQS resources first. Adding `@aws-sdk/client-s3` is the only blocker.
- `TestStatusCommandWorksWithNonDefaultPort` — publishes a container port on the
  `127.0.0.2` loopback alias, which Docker Desktop's VM networking rejects while a
  native Linux daemon accepts it. Better as a `requirement()`-gated test than a drop.

## Fixtures available for the remaining work

`support/`: `lstk()` and `lstkPty()` runners, `tempHome()`, `docker`,
`useExclusiveEmulator()`, `emulator-stub.ts` (stand-in container + log writing),
`fake-binary.ts` (fake `aws`/`terraform`/… recording argv, env, cwd),
`extension-fixture.ts`, `platform.ts` (`mockPlatform()` login flow + `fakeBrowser()`),
`license.ts`, `os-config-dir.ts`, `envelope.ts`, `requirements.ts`.

## Consequences worth accepting deliberately

- **The CLI boundary for the ported areas is now covered only by Node.** The e2e job has
  to be a required check.
- **Windows loses the login journey.** `pkg/browser` invokes `rundll32` there rather
  than a shimmable script. No loss against today: `login_test.go` already skips Windows.
- **Windows terminal coverage may improve** — node-pty drives ConPTY, where
  `creack/pty` has no Windows support at all. Unproven until the Windows CI leg reports.
- **Real-keyring coverage narrows to one journey run** (`keyring: "system"`, CI or
  opt-in). The adapter logic below it stays covered by the mocked unit tests in
  `internal/auth/token_storage_test.go`.
- **Container behaviour is verified on exactly one CI leg.** Ubuntu has Docker and the
  auth token, and `LSTK_E2E_REQUIRE_ALL=1` turns a missing prerequisite into a hard
  failure there; macOS and Windows runners cannot run Linux containers, so 63 and 72
  tests respectively skip. Roughly 51 tests could stop running on those legs without
  anything going red. A per-platform skip budget asserted in CI would close that gap.
