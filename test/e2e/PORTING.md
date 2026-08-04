# Porting status: Go integration suite → TypeScript e2e suite

**Intent:** this suite owns the CLI boundary for the areas listed under "Covered". Go
**unit** tests (`cmd/`, `internal/`) stay as they are — they are the right tool for logic
with no CLI surface. `test/integration` keeps everything under "Still owned by Go" and
does not go away wholesale; it shrinks as areas move across.

**Status: 219 tests across 26 files** (204 pass, 15 skip on this machine — an auth token,
a native Linux daemon and SSL_CERT_FILE certificate trust are the prerequisites not met
here). Full-suite wall clock ≈ 75s.

The Go integration suite has been trimmed accordingly: against `main` it goes from **419
→ 267** test functions across **54 → 40** files, 14,552 → 10,561 lines.

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
- **Token storage** — the keyring assertions across `login_test.go` and `logout_test.go`
  became behavioural ones in `login-journey.pty.test.ts` (see the README). "A failed
  login stores nothing" is asserted as *`start` still demands credentials and a retry
  reopens the browser* rather than by reading the store. Nothing in `test/e2e` touches
  credential storage; `keyring: "file" | "system"` only selects which backend the binary
  uses. That is not just tidiness — the two Go tests that did reach in
  (`TestLogoutCommandNotesWhenEmulatorStillRunning` and its multi-emulator sibling)
  wrote through the *test process's* `$HOME` while running lstk under a temp one, and
  passed on Linux CI while failing locally.

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
| Lifecycle (`stop`, `restart`, `status`, `reset`) | `stop-restart`, `status`, `reset.pty` | 28 |
| `logs`, `volume` | `logs.pty`, `volume.pty` | 21 |
| Config, completion, docs | `config`, `completion`, `docs` | 21 |
| Start paths, emulator selection, login journey, TUI | `start`, `start-local-image`, `emulator-select.pty`, `emulator-type`, `login-journey.pty`, `tui-runtime-error.pty` | 18 |
| `--endpoint-url` / `LSTK_ENDPOINT_URL` | `endpoint-url`(+`.pty`), `endpoint-url-https` | 36 |
| Harness self-tests (not product behaviour) | `harness/strip-ansi`, `harness/print-exactly` | 12 |

### What that removed from the Go suite

Deleted outright: `json_envelope`, `exit_code`, `non_interactive`, `completion`, `docs`,
`terraform_cmd`, `logs`, `reset`, `volume`, `stop`, `restart`, `logout`, `status`,
`endpoint_url`, `endpoint_url_https`.

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
| `config` | 1 of 12 | `TestConfigFlagEnvVarsPassedToContainer` inspects the container's environment |
| `emulator_type` | 7 of 10 | |
| `emulator_select` | 7 of 9 | |
| `start` | 34 of 36 | |
| `login` | 1 of 4 | `TestDeviceFlowSuccess` is the only remaining telemetry assertion for `login` |

## Still owned by Go

| Area | Go files | Cases | Would need |
| --- | --- | --- | --- |
| Snapshots | `snapshot_*_test.go`, `start_snapshot_test.go` | 78 | Mock cloud/S3 remotes; AWS SDK assertions against a live emulator |
| IaC end-to-end | `terraform_e2e`, `terraform_s3backend_e2e`, `cdk_*`, `sam_*` | 46 | Real terraform/cdk/sam installs (the `_cmd` half of terraform is done) |
| `start` remainder | `start_test.go`, `docker_unhealthy`, `docker_windows` | 41 | Never-healthy image via `docker commit`; bind/port introspection |
| Trimmed leftovers | `emulator_type`, `emulator_select`, `login`, `aws_cmd`, `json_flag`, `config` | 21 | See the table above — each has its own blocker |
| `az` proxy, `setup azure`, `awsconfig` | `az_*`, `setup_azure`, `awsconfig` | 22 | Isolated `~/.azure` assertions; `setup azure` completion marker |
| Extensions, signal forwarding | `extension`, `signal_forwarding` | 22 | Reference extension build; process-group signalling |
| Update & install | `update`, `multiple_installs`, `version_resolution` | 16 | Mock GitHub releases API; fake Homebrew/npm layouts |
| Telemetry, license, logging | `telemetry`, `license`, `logging`, `command_telemetry` | 15 | Mechanism by design — a mock analytics server and a mock license API, neither of which is user-observable |

## Fixtures available for the remaining work

`support/`: `lstk()` and `lstkPty()` runners, `tempHome()`, `docker`,
`useExclusiveEmulator()`, `emulator-stub.ts` (stand-in container + log writing),
`fake-binary.ts` (fake `aws`/`terraform`/… recording argv, env, cwd),
`extension-fixture.ts`, `platform.ts` (`mockPlatform()` login flow + `fakeBrowser()`),
`emulator-api.ts` (the emulator's own HTTP API, http or https, for `--endpoint-url`),
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
- **Two prerequisites are gated rather than assumed**, both `requirement()`-checked so a
  missing one skips instead of failing — and hard-fails on the CI leg that has
  everything (`LSTK_E2E_REQUIRE_ALL=1`). The https tests need certificate trust an
  exec'd lstk actually reads, which Go's x509 verifier only takes from SSL_CERT_FILE on
  Linux; the "status reports the bound port" test needs a daemon that can publish on the
  127.0.0.2 loopback alias, which Docker Desktop's VM networking refuses.
- **Container behaviour is verified on exactly one CI leg.** Ubuntu has Docker and the
  auth token, and `LSTK_E2E_REQUIRE_ALL=1` turns a missing prerequisite into a hard
  failure there; macOS and Windows runners cannot run Linux containers, so 63 and 72
  tests respectively skip. Roughly 51 tests could stop running on those legs without
  anything going red. A per-platform skip budget asserted in CI would close that gap.
