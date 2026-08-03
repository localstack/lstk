# Porting status: Go integration suite → TypeScript e2e suite

**Intent:** this suite replaces `test/integration/`. Go **unit** tests (`cmd/`,
`internal/`) stay as they are — they are the right tool for logic with no CLI surface.
`test/integration` and its separate Go module go away once the port lands.

**Status: 167 tests across 22 files** (159 pass, 8 skip on this machine), covering
roughly **130 of 384** Go test functions. Full-suite wall clock ≈ 165s.

## What "ported" means here

Not a line-by-line translation. Many Go integration tests assert mechanism rather
than behaviour, and those were deliberately **not** carried across:

- **Telemetry assertions** (`assertCommandTelemetry`, `mockAnalyticsServer`) — dropped
  throughout. What lstk sends to analytics is not something a CLI user observes.
- **Container introspection** (`docker inspect` of `Config.Env`, `HostConfig.Binds`)
  — replaced by the CLI-observable equivalent where one exists, e.g. `restart
  --persist` is asserted through the `• Persistence: Enabled` line rather than the
  container's environment.
- **Token storage** — five keyring assertions across `login_test.go` and
  `logout_test.go` became one behavioural journey (see the README).

Two Go tests turned out to be **vacuous** and were re-targeted rather than copied:
`TestConfigWithUnknownFieldsIsAccepted` and `TestConfigWithMissingOptionalTagSucceeds`
assert config acceptance through `lstk config path`, which never parses the file —
they pass against a nonexistent path. The ports use `lstk logout`, which really calls
`config.Get()`.

## Covered

| Area | Vitest files | Tests |
| --- | --- | --- |
| Proxy commands (`aws`, `terraform`) | `aws-proxy`, `terraform-proxy` | 41 |
| `--json` envelope, exit codes, `--non-interactive` | `json-envelope`(+`.pty`), `json-flag`, `exit-codes`, `non-interactive.pty` | 42 |
| Lifecycle (`stop`, `restart`, `status`, `reset`) | `stop-restart`, `status`, `reset.pty` | 22 |
| `logs`, `volume` | `logs.pty`, `volume.pty` | 20 |
| Config, completion, docs | `config`, `completion`, `docs` | 20 |
| Start paths, emulator selection, login journey, TUI | `start`, `start-local-image`, `emulator-select.pty`, `emulator-type`, `login-journey.pty`, `tui-runtime-error.pty` | 17 |
| Harness self-tests (not product behaviour) | `harness/strip-ansi` | 5 |

## Not yet ported

| Area | Go files | Cases | Needs |
| --- | --- | --- | --- |
| Snapshots | `snapshot_*_test.go`, `start_snapshot_test.go` | 78 | Mock cloud/S3 remotes; AWS SDK assertions against a live emulator |
| IaC end-to-end | `terraform_e2e`, `terraform_s3backend_e2e`, `cdk_*`, `sam_*` | ~46 | Real terraform/cdk/sam installs (the `_cmd` half is done) |
| `start` remainder | `start_test.go`, `docker_unhealthy`, `docker_windows` | ~38 | Never-healthy image via `docker commit`; bind/port introspection |
| `az` proxy, `setup azure`, `awsconfig` | `az_*`, `setup_azure`, `awsconfig` | ~23 | Isolated `~/.azure` assertions; `setup azure` completion marker |
| Extensions, signal forwarding | `extension`, `signal_forwarding` | 20 | Reference extension build; process-group signalling |
| Update & install | `update`, `multiple_installs`, `version_resolution` | 16 | Mock GitHub releases API; fake Homebrew/npm layouts |
| Telemetry, license, logging | `telemetry`, `license`, `logging` | 14 | Mostly mechanism — decide per test what user-visible behaviour is worth keeping |

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

- **The CLI boundary ends up covered only by Node.** The e2e job has to be a required
  check before `test-integration` is deleted, not after.
- **Windows loses the login journey.** `pkg/browser` invokes `rundll32` there rather
  than a shimmable script. No loss against today: `login_test.go` already skips Windows.
- **Windows terminal coverage may improve** — node-pty drives ConPTY, where
  `creack/pty` has no Windows support at all. Unproven until the Windows CI leg reports.
- **Real-keyring coverage narrows to one journey run** (`keyring: "system"`, CI or
  opt-in). The adapter logic below it stays covered by the mocked unit tests in
  `internal/auth/token_storage_test.go`.
