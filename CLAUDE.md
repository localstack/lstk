# Project Overview

lstk is LocalStack's new CLI (v2) - a Go-based command-line interface for starting and managing LocalStack instances via Docker (and more runtimes in the future).

# Developer Setup

After cloning, install the pre-commit hooks:

```bash
pre-commit install
```

This installs a [gitleaks](https://github.com/gitleaks/gitleaks) hook that scans staged files for secrets before each commit. Requires [pre-commit](https://pre-commit.com/#install).

# Build and Test Commands

```bash
make build              # Compiles to bin/lstk
make test               # Run unit tests (cmd/ and internal/) via gotestsum
make test-integration   # Run integration tests (rebuilds bin/lstk via `build`, requires Docker)
make lint               # Run golangci-lint (version pinned via .tool-versions)
make govulncheck        # Run govulncheck (reachability-based vuln scan)
make mock-generate      # Regenerate mocks (mockgen via go:generate)
make clean              # Remove build artifacts
```

Run a single unit test:
```bash
go test ./internal/<pkg>/ -run TestName
```

Run a single integration test:
```bash
make test-integration RUN=TestStartCommandSucceedsWithValidToken
```

Notes:
- Integration tests require `LOCALSTACK_AUTH_TOKEN` environment variable for valid token tests.
- `test/integration` is a **separate Go module** (own `go.mod`); `make lint` runs golangci-lint twice — repo root and `test/integration` — and fails if the installed golangci-lint version doesn't match `.tool-versions`. `golangci-lint run --fix` auto-fixes many findings.
- `make govulncheck` also runs twice for the same reason (root + `test/integration`). It complements the dependency-version scan in `trivy.yml` with call-graph reachability analysis — it only flags known vulnerabilities in code actually called from the repo. It has no severity filter (most Go vulnerability reports carry no CVSS data), so it gates on reachability alone: any reachable known vulnerability fails the job. CI (`ci.yml`'s `govulncheck` job) runs it on every push/PR and uploads a SARIF report to the Security tab, same pattern as Trivy, but it is **not yet** in `release`'s `needs:` — it's a new check on a staged rollout and should be promoted to a hard release gate once it's proven false-positive-free.
- Mocks are generated with mockgen (go.uber.org/mock) via per-file `//go:generate mockgen ...` directives (e.g. `internal/snapshot/remote.go`); adding a mock means adding a directive, then `make mock-generate`.
- Set `CREATE_JUNIT_REPORT=1` to get a JUnit XML report from `make test` / `make test-integration`.

# Architecture

- `main.go` - Entry point
- `cmd/` - CLI wiring only (Cobra framework), no business logic; one file per command
- `internal/` - All business logic goes here
  - `api/` - LocalStack platform API client (auth, license)
  - `auth/` - Authentication (env var token or browser-based login), token storage/keyring
  - `awscli/`, `azurecli/`, `eksctl/` - Exec wrappers behind the `lstk aws` / `lstk az` / `lstk eksctl` proxy commands
  - `awsconfig/` - AWS CLI profile management in `~/.aws/` (`lstk setup aws`)
  - `azureconfig/` - Azure CLI cloud registration and interception (`lstk setup azure`, `lstk az`)
  - `caller/` - Classifies the invoking caller/harness (human vs agent) for telemetry
  - `config/` - Viper-based TOML config loading and path resolution
  - `container/` - Handling different emulator containers (start flow, gateway ports, offline fallbacks)
  - `emulator/` - Emulator API abstraction with per-type implementations (`aws/`, `azure/`, `snowflake/`)
  - `endpoint/` - Emulator endpoint/host resolution
  - `env/` - Process environment snapshot/injection helper (also used to isolate test envs)
  - `extension/` - Git-style `lstk-<name>` extension resolution and exec
  - `iac/` - Wrappers for third-party infrastructure as code tools (`terraform/`, `cdk/`, `sam/`)
  - `log/` - Internal diagnostic logging (not for user-facing output — use `output/` for that)
  - `output/` - Generic event and sink abstractions for CLI/TUI/non-interactive rendering
  - `ports/` - Port availability checks
  - `proc/` - Runs wrapped external tools (`aws`, `terraform`, `cdk`, `sam`, `az`, extensions) with signal forwarding instead of `cmd.Run()` — see Signal Forwarding to Wrapped Tools below
  - `reset/` - `lstk reset` domain logic
  - `runtime/` - Abstraction for container runtimes (Docker, Kubernetes, etc.) - currently only Docker implemented
  - `snapshot/` - Snapshot save/load/list/remove/show domain logic
  - `telemetry/` - CLI analytics events client
  - `terminal/` - Plain-mode terminal helpers (spinner, TTY detection)
  - `tracing/` - OpenTelemetry setup (`LSTK_OTEL=1`)
  - `ui/` - Bubble Tea views for interactive output
  - `update/` - Self-update logic: version check via GitHub API, binary/Homebrew/npm update paths, archive extraction; the binary path verifies the downloaded archive's SHA-256 against the release's `checksums.txt` before replacing the executable (hard fail on missing/malformed manifest or mismatch)
  - `validate/` - Reusable input validators for user-supplied CLI values (pod names, env var names, auth tokens) rejecting malformed/hostile input (control chars, path traversal, percent-encoding, shell metacharacters)
  - `version/` - Version info
  - `volume/` - `lstk volume` domain logic

Commands are registered in `cmd/root.go` in two Cobra groups: the `commands` group (start, stop, restart, login, logout, status, logs, setup, config, volume, update, docs, snapshot, reset, save, load) and the `tools` group of proxy commands (aws, terraform/tf, cdk, sam, az, eksctl). Shared helpers: `cmd/root.go` (wiring, groups, `requireSubcommand`, `initConfig`), `cmd/help.go` (help template), `cmd/iac.go` (IaC command boundary), `cmd/extension.go` (extension dispatch).

## Container runtime discovery

When `DOCKER_HOST` isn't set, `DockerRuntime` resolves the daemon endpoint in order: explicit `DOCKER_HOST` (always wins, unconditionally — it is checked in `NewDockerRuntime` as well as via `client.FromEnv`, because the `WithHost` applied for a detected endpoint would otherwise silently replace it) > `DOCKER_CONTEXT` or the current Docker CLI context (if non-default and dialable — a stale/unreachable context falls through rather than hard-failing) > a live `/var/run/docker.sock` **on Linux only** (a co-installed runtime's socket must not outrank Docker for a Docker-API tool; on macOS/Windows that path is the active VM runtime's symlink, so the VM probes stay first there to keep `isVM`/`Flavor` classification and the bind-mount rewrite correct) > a probe list of known sockets (Docker Desktop, Rancher Desktop, Colima, OrbStack, Podman machine on macOS, Lima, then native Podman on Linux) > the Docker SDK's own default. Probing dials as well as stats, so a leftover socket file never shadows a live one. The ordering is exercised through `findDockerSocketFor`, which takes the native socket path and GOOS as parameters so tests don't depend on whether the host running them has a Docker daemon. The "runtime unavailable" error also tailors its suggested start command (`rdctl start`, `colima start`, `podman machine start`, etc.) to whichever runtime it detects. Logic lives in `internal/runtime/docker.go`, `internal/runtime/docker_context.go`, and `internal/runtime/flavor.go`. User-facing per-runtime setup notes: [docs/container-runtimes.md](docs/container-runtimes.md).

# Commits, PRs, and Linear

- Commit messages: a single concise line. Add a `Co-Authored-By: Claude <noreply@anthropic.com>` trailer to commits and PR bodies.
- Never commit or push unless explicitly asked.
- PRs are squash-merged; titles start with an action verb and stay under ~70 characters.
- Every PR needs exactly one `semver:` label (`patch`/`minor`/`major`) and one `docs:` label (`skip`/`needed`) — enforced by `check-release-label.yml`. Use `/create-pr` to scaffold title, body, and labels.
- PR descriptions follow the Motivation / Solution / Docs / Review structure (template in the `/create-pr` skill): a high-level description of the issue or feature, a solution summary a reviewer can thumbs-up or -down, and a collapsible Docs section technical writers triage from — always present, stating explicitly when there is nothing to document.
- Issues and tickets live in Linear, not GitHub Issues. Typical flow: Linear issue → branch named from the issue (e.g. `devx-123-...`) → PR body ends with `Closes DEVX-123` (or `Towards DEVX-123` if partial). Ask which Linear team if unclear (e.g. PRO = product, DEVX = developer experience).
- Small PRs and straightforward bug fixes may merge without a human approval when the author is confident; bigger features/PRs still need review and an approval, as usual. This shifts weight onto self-review rather than lowering the bar — before treating any PR-sized change as done, run `/review-pr` against it, confirm tests pass, and add integration tests per the Testing section below. Before creating a PR, say in the session whether a human review looks advisable and why, and add a short "Review" line in the PR description itself (new/changed user-facing behavior, undiscussed or speculative work → advise review; straightforward, small, already-discussed → self-merge candidate) so the assessment is visible to both the author and anyone reading the PR, not just implied. If unsure, advise review.

# Release Process

Releases are automated: a weekly workflow (`.github/workflows/automated-release.yml` → `create-release-tag.yml`) tags and publishes via goreleaser, deriving the version bump from merged PRs' `semver:` labels. See `docs/RELEASING.md`.

# Logging

lstk always writes diagnostic logs to `$CONFIG_DIR/lstk.log` (appends across runs, cleared at 1 MB). Two log levels: `Info` and `Error`.

- `log.Logger` is injected as a dependency (via `StartOptions` or constructor params). Use `log.Nop()` in tests.
- This is separate from `output.Sink` — the logger is for internal diagnostics, the sink is for user-facing output.

# Configuration

Uses Viper with TOML format. lstk uses the first `config.toml` found in this order:
1. `./.lstk/config.toml` (project-local)
2. `$HOME/.config/lstk/config.toml`
3. **macOS**: `$HOME/Library/Application Support/lstk/config.toml` / **Windows**: `%AppData%\lstk\config.toml`

When no config file exists, lstk creates one at `$HOME/.config/lstk/config.toml` if `$HOME/.config/` already exists, otherwise at the OS default (#3). This means #3 is only reached on macOS when `$HOME/.config/` didn't exist at first run.

Use `lstk config path` to print the resolved config file path currently in use.
When adding a new command that depends on configuration, wire config initialization explicitly in that command (`PreRunE: initConfigDeferCreate`). Keep side-effect-free commands (e.g., `version`, `config path`) without config initialization.

A parent command that only groups subcommands (e.g. `config`, `setup`, `volume`, `snapshot`) must call `requireSubcommand(cmd)` (in `cmd/root.go`). Cobra otherwise prints help and exits 0 for an unknown/missing subcommand of a non-runnable parent; `requireSubcommand` sets `cobra.NoArgs` plus a help-printing `RunE` so a bare invocation still shows help (exit 0) while an unknown subcommand exits non-zero. Cobra's autogenerated `completion` command is the same shape, but it is created lazily during `Execute`, so `NewRootCmd` calls `root.InitDefaultCompletionCmd()` to materialize it before applying `requireSubcommand` (the call is idempotent — Cobra skips re-adding it).

Created automatically on first run with defaults. Supports emulator types: `aws`, `snowflake`, and `azure`.

`initConfigDeferCreate` (wrapping `config.Load`) only ever *reads* config — it never writes the default config.toml to disk. That's deliberate: the emulator-selection prompt (`container.SelectEmulator`) is shown only when `firstRun` is still true, and only bare `lstk` and `lstk start` wire it in (`NeedsEmulatorSelection: firstRun` in `startEmulator`). If some other command eagerly persisted a default (`type = "aws"`) config on its own first run, the selector would never get a chance to show on a genuinely fresh install — every command must use `initConfigDeferCreate`, never a hypothetical eager-create variant, so that only a real emulator start (interactive selection, or the non-interactive default-emulator path) ever writes the file. `EnsureCreated()` therefore has exactly three legitimate callers: the non-interactive first-run path in `cmd/root.go` (after a successful default start), `container.SelectEmulator` (after the user picks one), and `container.ApplyEmulatorType` (the `--type` flag's first-run path).

Only one `[[containers]]` block may be enabled at a time. `container.Start` rejects a config with more than one block up front (before health/auth checks and image pulls), since running multiple emulators together (e.g. AWS + Snowflake) is unsupported and would otherwise fail later during startup with container-name conflicts or port collisions. The guard lives on the start path (not `config.Get()`) on purpose: recovery/reporting commands like `stop`, `status`, and `logout` must still enumerate multiple running emulators.

Each `[[containers]]` block may set an optional `container_name` (override the derived container name; also what the emulator reports as `MAIN_CONTAINER_NAME`), an optional `image` (override the default Docker Hub image), a `volumes` list of Docker-style bind specs (persistence dir, init hooks, arbitrary mounts), and an `expose_ports` list publishing container ports the gateway/service ranges don't cover (e.g. `expose_ports = [53]` for the emulator's DNS server). Container-name derivation and its deliberate decoupling from the default persistence directory, image/tag precedence, `volume` vs `volumes` semantics, path-resolution rules, and the `expose_ports` grammar are documented on `config.ContainerConfig` and its methods (`internal/config/containers.go`); the user-facing summary is `internal/config/default_config.toml`.

## Selecting the emulator (`--type`)

`lstk start --type <aws|snowflake|azure>` (shorthand `-t`; also on the bare root) is the non-interactive answer to the first-run emulator picker. It is a flag only — a positional (`lstk start azure`) is rejected with a hint pointing at `--type`, to avoid implying the root-level `lstk aws`/`lstk az` proxy names mean "start that emulator". It is defined as "rewrite the `type` line in config", not an ephemeral per-run override — downstream commands (`stop`, `status`, `logs`, `volume`, snapshot auto-load) all resolve from the configured type, so persisting keeps config and reality in sync. First run creates the config with the selected type (same `EnsureCreated`/`SetEmulatorType` path the picker uses); a matching config is a no-op; a differing config is switched in place via the surgical type-line rewrite (comments/formatting preserved) with a note naming the file. On switch: a custom `image` is a hard error (it pins a product that can't be reinterpreted under a new type — use `--config` for a separate profile), a non-`latest` `tag` and any `volumes`/`volume` are kept with a warning, and `container_name`/`port`/`env`/`snapshot` are kept silently (they describe the user's topology rather than pinning a product). Domain logic is `container.ApplyEmulatorType` (parallel to `container.SelectEmulator`); it is applied at the top of `startEmulator` (`cmd/root.go`) before snapshot/start-options are resolved, so it runs before the TUI and its messages go through a plain sink.

`GATEWAY_LISTEN` (host exposure and published ports) is read from the container's resolved env, not hardcoded; parsing and derivation live in `internal/container/gateway.go`.

# Offline / Enterprise Environments

There is no `--offline` flag. Instead `container.Start` degrades gracefully when internet requests fail (Docker Hub unreachable, proxy/TLS interception, license server unreachable): local images are used when pulls fail, and the license pre-flight is skipped on transport-level failures, non-definitive server responses (5xx/407), or unsupported-tag rejections so the container validates its own bundled license. Definitive license rejections (HTTP 400/401/403) drop the cached license and offer an in-place re-login instead of requiring a manual `lstk logout` (DEVX-658). The exact fallback and retry rules live in `tryPrePullLicenseValidation`/`validateLicense`/`startWithLicenseRetry` (`internal/container/start.go`); pair them with a custom `image` in the config to point at a locally loaded image or an internal-registry mirror.

# Targeting an External Emulator (`--endpoint-url`)

`--endpoint-url <url>` (a root persistent flag) lets `aws`, `az`, `terraform`/`tf`, `cdk`, `sam`, `snapshot save`/`load` (incl. the `lstk save`/`lstk load` aliases), `snapshot remove`, `snapshot list s3://...`, `reset`, and `status` target an emulator lstk did not start — docker compose, host-network mode, CI, a different machine, or a LocalStack cloud-hosted **ephemeral instance** — instead of discovering one via local Docker. `LSTK_ENDPOINT_URL` is the equivalent environment variable. Both `http://` and `https://` are accepted (any other scheme is rejected); the resolved scheme is preserved end-to-end all the way to whatever ultimately makes the request — the wrapped `aws`/`terraform`/`cdk`/`sam` tools, and the emulator API calls behind `snapshot`/`reset`/`status` — never normalized to `http`, which is what makes `https://` ephemeral instances work. 

`lstk logs`, `stop`, `restart`, `volume` (`path`/`clear`), `start`, and the bare `lstk` root command (which starts the emulator the same way `start` does) have no remote equivalent and reject *any* resolved endpoint source.

Emulator type (aws/azure/snowflake) is always auto-detected by probing `/_localstack/health` (falling back to `/_localstack/info` for Azure, whose health response omits `version`) — there is no manual override flag or config setting; an inconclusive result is a hard failure. `terraform`/`cdk`/`sam` (AWS-only) reject a detected non-AWS type with the same error shape used for a wrong locally-running emulator.

# Emulator Setup Commands

Use `lstk setup <emulator>` to set up CLI integration for an emulator type:
- `lstk setup aws` — Sets up an AWS CLI `localstack` profile in `~/.aws/config` and `~/.aws/credentials`. Runs interactively (Y/n prompt) on a TTY; in non-interactive mode (CI / piped / `--non-interactive`) it writes the profile with defaults and exits 0 without prompting, and returns write/check failures as errors so automation exits non-zero. Overwriting an existing `localstack` profile whose values differ requires `--force`. Shared host resolution lives in `awsconfig.ResolveProfileHost`; the non-interactive write is `awsconfig.SetupNonInteractive`, the interactive path is `awsconfig.Setup(..., skipConfirm)`.
- `lstk setup azure` (alias `lstk setup az`) — Prepares an isolated Azure CLI config dir pointing at the LocalStack Azure emulator; the user's global `~/.azure` is untouched. `lstk az <args>` then runs `az` against that isolated dir. `lstk az start-interception` / `stop-interception` are the opt-in global mode that mutates `~/.azure` so plain `az` targets LocalStack. Mechanics and extension points: `internal/azureconfig/azureconfig.go` (`Env`, `BuildCloudConfig`) and `interception.go`.

This naming avoids AWS-specific "profile" terminology and uses a clear verb for mutation operations.

Environment variables:
- `LOCALSTACK_AUTH_TOKEN` - Auth token (skips browser login if set). It takes precedence over credentials stored in the keyring, so a per-invocation token overrides a previous `lstk login` without a `lstk logout` first; resolution order is env var → keyring → browser login (`auth.GetToken`, mirrored in `cmd/root.go`'s telemetry token resolution).
- `LSTK_STARTUP_TIMEOUT` - Startup readiness deadline for `lstk start` (Go duration). Zero/unset uses the per-mode default resolved in `resolveStartupTimeout` (`internal/container/start.go`): 20s interactive (deadline only shows a recoverable keep-waiting/stop prompt, re-armed by "keep waiting"), 60s non-interactive (fatal; the container is left running for inspection). Container exits are detected separately — and instantly, with the exit code — via the exit wait `runtime.Runtime.Start` registers between create and start. `lstk start --timeout <duration>` (also on the bare root) overrides this for a single run; the flag wins over the env var when explicitly set, and `--timeout 0` falls back to the per-mode default (`addTimeoutFlag`/`applyTimeoutFlag` in `cmd/root.go`). `restart` and the snapshot auto-start path do not expose the flag.
- `LSTK_OTEL=1` - Enables OpenTelemetry trace export (disabled by default); when enabled, standard `OTEL_EXPORTER_OTLP_*` env vars are respected by the SDK. Requires an OTLP-compatible backend to receive and visualize telemetry — for local development, `make otel` starts one (UI at http://localhost:16686).
- `LSTK_MERGE_STRATEGY` - Default merge strategy for `snapshot load` / `load` (`account-region-merge`, `overwrite`, or `service-merge`) when `--merge` is not passed; an explicit `--merge` always wins. Resolved in `resolveMergeStrategy` (`cmd/snapshot.go`).

# Infrastructure as Code Commands

lstk proxies third-party IaC tools at the AWS emulator so they run against LocalStack with no `*local` wrapper installed. Each command forwards its args to the real tool after configuring the environment; domain logic lives under `internal/iac/<tool>/cli/`, wiring in `cmd/<tool>.go`, with shared command-boundary helpers in `cmd/iac.go`. Siblings: `lstk terraform` (alias `tf`), `lstk cdk`, `lstk sam`.

# Account Selection Across the AWS-Family Proxies

LocalStack derives the AWS account from the access key id it receives (12 digits → that account, anything else → `000000000000`), so account selection is a matter of controlling `AWS_ACCESS_KEY_ID` for the wrapped tool. `lstk aws`, `lstk terraform`, and `lstk sam` all accept `--account <12 digits>` in leading position — between the command name and the wrapped tool's action — falling back to the ambient `AWS_ACCESS_KEY_ID`, then to `test`. A flag placed *before* the command name is rejected with a placement error rather than silently eaten by Cobra. `lstk cdk` rejects `--account` outright: CDK resolves the account through an STS round-trip behind its own account cache, which did not track the flag reliably.

Shared plumbing lives in `cmd/proxy.go` (`leadingFlags`, `stripLeadingProxyFlags`, `rejectPreSubcommandFlags`, `resolveAccountSelection`), not `cmd/iac.go` — `lstk aws` is not an IaC command but shares all of it. The 12-digit rule is `validate.AWSAccountID`; `awsconfig.DeactivateAccessKey` neutralizes a real-looking ambient `AKIA…`/`ASIA…` key (leading `A` → `L`) so a live credential never reaches the emulator. It sits in `internal/awsconfig/credentials.go` beside the other credential-value helpers because all three account-selecting proxies use it through the shared command boundary — it belongs to no single proxy's package.

`lstk terraform`'s S3 backend provisioning shells out to the `aws` CLI and must use the same resolved account as the generated backend block (`provisionEnv` in `internal/iac/terraform/cli/provision.go`) — LocalStack partitions resources by account, so a bucket provisioned elsewhere is invisible to the `terraform init` that follows.

# eksctl Proxy

`lstk eksctl` proxies [eksctl](https://eksctl.io/) at the AWS emulator, replacing the manual `AWS_*_ENDPOINT` exports from the "Newer Versions" flow in the LocalStack eksctl docs. Domain logic is `internal/eksctl/` (an exec wrapper like `awscli`/`azurecli`, not an IaC tool), wiring in `cmd/eksctl.go`. It sets the CloudFormation, EC2, EKS, ELB, ELBv2, IAM, and STS service endpoints (`internal/eksctl/env.go`) to the resolved LocalStack endpoint, strips ambient AWS profile/session config, and defaults credentials/region only when absent. It gates on eksctl >= 0.181.0 (`internal/eksctl/version.go`), the version from which eksctl honors those endpoint variables — older versions are rejected rather than silently targeting real AWS (same rationale as the sam version gate). Offline subcommands (`version`, `info`, `completion`) and `--help` run without Docker or a running emulator; everything else requires the AWS emulator via the shared `requireRunningAWSEmulator`/`resolveAWSContainer` helpers in `cmd/iac.go`. `LSTK_EKSCTL_CMD` overrides the binary name.

# Extensions

lstk supports Git-style extensions: when `lstk <name>` is not a built-in command or alias, lstk resolves and execs an external `lstk-<name>` executable, forwarding arguments verbatim and propagating the exit code. Built-ins always win. Resolution order is built-ins → bundled dir (the directory of the symlink-resolved lstk executable) → `PATH`; there is no manifest. Runtime context is conveyed via `LSTK_EXT_API_VERSION` and `LSTK_EXT_CONTEXT` (JSON: `configDir`, optional `authToken`, `nonInteractive`, `json`, optional `sessionId` — lstk's telemetry session id, omitted when telemetry is disabled, so an extension's own telemetry can join lstk's `ext:<name>` event — optional `machineId` — lstk's anonymized machine id (the prepared hash), omitted alongside `sessionId` when telemetry is disabled, so an extension reports the same machine without re-deriving it — optional `endpointUrl` — the resolved `--endpoint-url`/`LSTK_ENDPOINT_URL`/`AWS_ENDPOINT_URL` value, conveyed verbatim and unvalidated (dispatch never rejects or probes it, unlike the built-ins' `rejectEndpointURL`), omitted when no source is set — and an `emulators` array, which stays local-Docker discovery and is independent of `endpointUrl`) — see `extension.Context`/`Environ` in `internal/extension/context.go`; dispatch and help listing are in `cmd/extension.go`. Automated distribution/co-update of bundled extensions is deferred to the `add-bundled-extension-distribution` change. See [extensions-authoring.md](docs/extensions-authoring.md) for the author-facing contract.

# Signal Forwarding to Wrapped Tools

Wrapped external tools (`aws`, `terraform`, `cdk`, `sam`, `az`, `eksctl`, and extensions) are run through `proc.Run(cmd)` (in `internal/proc/run.go`) rather than `cmd.Run()`. These execs are created with `exec.CommandContext` using lstk's root context, which is cancelled on `SIGINT`/`SIGTERM`; `exec.CommandContext`'s default `Cancel` would then SIGKILL the child immediately, denying tools like `terraform apply` the chance to clean up (e.g. release the state lock). `proc.Run` disarms that (its `Cancel` returns `os.ErrProcessDone`, which both suppresses the kill and avoids injecting `context.Canceled` into the wait result, preserving the tool's real exit code) and instead lets the tool terminate from the signal it receives, waiting for it to finish its own shutdown. Forwarding is per-signal: `SIGTERM` is always relayed to the child (a terminal never generates it, so `kill <lstk-pid>` / `timeout` / an IDE stop button would otherwise never reach the tool), while `SIGINT` is relayed only when none of lstk's std streams is a terminal — an attached terminal already delivers Ctrl-C to the child via the foreground process group, and a second near-simultaneous SIGINT makes tools like terraform abort immediately instead of cleaning up. The any-stream check matters: with only stdin redirected (`yes | lstk terraform apply`) lstk still sits in the terminal's foreground process group. This differs from `npm/launcher.js`, which forwards unconditionally — safe there because its child is lstk itself, which tolerates duplicate signals; wrapped tools do not. Short internal captured-output execs (version checks, schema discovery, backend provisioning) still use `cmd.Run()` directly. End-to-end signal tests live in `test/integration/signal_forwarding_test.go`, backed by the reference extension's `signal-wait` mode.

When lstk's stdout and stderr are both terminals, `lstk aws` runs the child via `proc.RunInPTY` (in `internal/proc/pty.go`) instead: the spinner path wraps the child's output in an `io.Writer`, which makes os/exec hand the child a pipe, and the frozen Python aws CLI then block-buffers stdout (8 KB, ignoring `PYTHONUNBUFFERED`) — streaming commands like `aws logs tail --follow` showed nothing until exit (DEVX-1026). The PTY makes the child see a terminal (line-buffered, colored output; stdout/stderr merged, no new session so Ctrl-C still reaches it via the process group) while the master side is copied through the spinner's `StopOnWriteWriter`. Falls back to plain `proc.Run` when no PTY can be allocated (Windows), and stays on pipes whenever stdout is redirected so pipelines never receive colors/CRLF.

`lstk az` has the identical wrapper pattern and gets the identical treatment (DEVX-1028): `azurecli.Exec` takes the same `usePTY` parameter under the same both-streams-are-terminals condition, and `azurecli`'s own `execEnv` injects `PYTHONUNBUFFERED=1` (leaving a user-set value alone). Unlike the frozen aws v2 binary, every `az` distribution runs a real Python interpreter, so that env var is effective there — the PTY additionally makes az see a terminal, which is what its own terminal-gated output depends on. `azurecli.Run` (short captured-output execs behind `setup azure`/interception) always passes `usePTY: false`.


# Snapshots

`lstk snapshot` captures and restores the running emulator's state (for Snowflake and Azure a heads-up is shown that results may be incomplete). Domain logic lives in `internal/snapshot/`; `cmd/snapshot.go` is wiring + output-mode selection. Top-level `lstk save` / `lstk load` are aliases for the save/load subcommands.

- `lstk snapshot save [destination]` (`-s`/`--services`: comma-separated list to limit a save to a subset of services; applies uniformly to local files, `pod:` cloud snapshots, and S3-remote saves) / `lstk snapshot load REF` (`--merge`: `account-region-merge` default, `overwrite`, `service-merge`) / `list` (cloud; `--all` for org-wide) / `remove REF` / `show REF` / `versions REF`.
- `show`, `versions`, and bare `list` (no `s3://` arg) only ever call the LocalStack platform API — never the emulator. `remove`, by contrast, proxies its delete through the running emulator despite being a "cloud" (`pod:`) concept, so it still requires one to be reachable; don't conflate "operates on cloud-hosted storage" with "never touches the emulator" — they're different axes.
- A REF is a local `.snapshot` file, a `pod:` cloud snapshot on the LocalStack platform (requires auth), or an `s3://bucket/prefix` remote in the user's own bucket (the emulator performs the transfer; S3 supports save/load/list only).
- Every save to an existing `pod:` snapshot creates a new **version**. `versions REF` lists them; `load` and `show` accept a `pod:<name>:<version>` REF (latest when omitted). `save`, `remove`, `versions`, and S3 remotes reject a version suffix rather than ignore it.
- A `[[containers]]` block (AWS only) can set `snapshot = "pod:..."` to auto-load after a fresh start; `lstk start --snapshot REF` overrides it for one run, `--no-snapshot` skips it.
- `save`/`load`/`remove` and `list s3://...` support the global `--endpoint-url` targeting described under "Targeting an External Emulator"; `show`, `versions`, and bare `list` silently ignore it (they never touch the emulator regardless).

REF parsing helpers live in `internal/snapshot/destination.go`; S3 credential precedence and remote-upsert mechanics in `internal/snapshot/remote.go` and `resolveS3Credentials` (`cmd/snapshot.go`); the auto-load wiring in `resolveStartSnapshotRef`/`newSnapshotAutoLoader` (`cmd/snapshot.go`).

# NPM Distribution

`@localstack/lstk` is published as a thin Node wrapper package whose `bin` is `npm/launcher.js`. The wrapper resolves the prebuilt Go binary from the platform-specific optional dependency npm installed for the host, execs it, and **forwards `SIGINT`/`SIGTERM`/`SIGHUP`** so a programmatic `kill` of the Node process tears down the Go child instead of orphaning it (the auto-generated wrapper from `goreleaser-npm-publisher` installed no signal handlers). The launcher also propagates the child's exit code / terminating signal. Tests in `npm/launcher.test.js` run via `node --test` in the `test-launcher` CI job.

The release job (`.github/workflows/ci.yml`) builds the npm packages with `goreleaser-npm-publisher build`, overwrites the generated `dist/npm/lstk/index.js` with `npm/launcher.js`, then `npm publish`es each package — replacing the previous single `evg4b/goreleaser-npm-publisher-action` step.

# Code Style

- Don't add comments for self-explanatory code. Only comment when the "why" isn't obvious from the code itself.
- Do not remove comments added by someone else than yourself.
- Errors returned by functions should always be checked unless in test files.
- Terminology: in user-facing CLI/help/docs, prefer `emulator` over `container`/`runtime`; use `container`/`runtime` only for internal implementation details.
- Docker image naming convention (use these names consistently for variables/params/fields):
  - **full image** (`image`, `imageName`): full reference with registry and tag, e.g. `"localstack/snowflake:latest"`. Used by Docker SDK calls (`PullImage`, `GetImageVersion`).
  - **image repo** (`imageRepo`, `imageRepos`): registry/name without tag, e.g. `"localstack/snowflake"`. Used by `FindRunningByImage` and image-matching helpers.
  - **product name** (`productName`, `ProductName`): name only, no registry, no tag, e.g. `"localstack-pro"` / `"snowflake"`. Used for license API `ProductInfo.Name` and to build full images via `dockerRegistry + "/" + ProductName`.
- Avoid package-level global variables. Use constructor functions that return fresh instances and inject dependencies explicitly. This keeps packages testable in isolation and prevents shared mutable state between tests.
- Never print directly to stdout/stderr (e.g., `fmt.Fprintf(os.Stderr, …)`). For user-facing output, emit events through `output.Sink`. For internal diagnostics, use `log.Logger`. If neither is available (e.g., during logger setup), return errors to the caller and let them decide.
- Don't deprecate commands with Cobra's `Deprecated` field: it prints the notice raw to `os.Stderr` (bypassing `output.Sink`) and silently hides the command from `--help` and generated `lstk docs`. Remove the old command outright instead; if a transition period is genuinely needed, keep the command visible and emit the deprecation notice through the sink.
- Do not call `config.Get()` from domain/business-logic packages. Instead, extract the values you need at the command boundary (`cmd/`) and pass them as explicit function arguments. This keeps domain functions testable without requiring Viper/config initialization.
- Validate user/agent-supplied values where they are first accepted (the command boundary, or the domain parser that owns the format) via `internal/validate` — never an inline one-off regexp. Route by value class: pod names → `validate.PodName`; opaque secrets → only loose malformed-ness checks (`validate.AuthToken` style — no charset restriction); paths and URLs → their existing parsers (`filepath`, `net/url`). For other identifiers, follow the owning API's documented contract and add a dedicated validator if needed. If no validator fits, add one to `internal/validate` with rule-code tests instead of forking rules locally — parallel validators for the same value class drift (the pod-name rules forked exactly this way before #293 re-unified them).

# Shell Completion

Cobra's generated bash completion script requires `_get_comp_words_by_ref` from the bash-completion package on both of its init paths, and stock macOS (bash 3.2) ships without that package — so completion failed with "command not found" on every Tab (DEVX-950). `selfContainBashCompletion` in `cmd/completion.go` wraps the autogenerated `completion bash` command to prepend a guarded pure-bash fallback (defined only when the package is absent, the git-completion.bash approach) and replaces the help text. The fallback body must stay bash 3.2 compatible (no `declare -A`, namerefs, `mapfile`, case-conversion expansions). It covers only `_get_comp_words_by_ref`; Cobra's script still calls bash-completion's `_filedir` for `ShellCompDirectiveFilterFileExt`/`ShellCompDirectiveFilterDirs` (`MarkFlagFilename`/`MarkFlagDirname`) and the ActiveHelp second-Tab path — lstk uses none of these today, so adopting one means growing the fallback. In docs/help, never recommend `source <(lstk completion bash)` — it is a silent no-op on bash 3.2; recommend `eval "$(lstk completion bash)"` instead. Zsh/fish/powershell scripts are self-contained upstream and untouched.

`lstk aws <TAB>` completes AWS services/operations/parameters by delegating to the AWS CLI's own `aws_completer` from a `ValidArgsFunction` on the `aws` command (DEVX-846) — `awscli.Complete` in `internal/awscli/complete.go`, wired in `cmd/aws.go`. Going through Cobra rather than registering `complete -C aws_completer lstk` is what makes it work in every shell `lstk completion` supports (the native registration is bash/zsh-only) and keeps the bash fallback above unchanged. `aws_completer` speaks bash's `complete -C` protocol: `COMP_LINE`/`COMP_POINT` in, candidates one-per-line out. Two constraints it imposes: the line must start with the literal word `aws` (the completer drops the first word before matching, so `lstk aws s3 l` returns nothing), and `COMP_POINT` is a **character** offset, not a byte one. Cobra never runs `PreRunE` on the `__complete` path, so completion stays offline — no config load, no Docker health check, no endpoint resolution — which matches the completer, which never contacts an endpoint. A missing or failing completer must return `ShellCompDirectiveDefault` and print nothing: any output on this path is read by the shell as a candidate. The Tab-press timeout is set by the caller in `cmd/aws.go` (`awsCompletionTimeout`), not inside `awscli.Complete` — an internal deadline made the unit test flaky on cold CI runs. That deadline only actually bounds a Tab press because `Complete` also sets `cmd.WaitDelay`: `exec.CommandContext` kills the completer on expiry but does not close a pipe the completer's own children still hold, so a completer leaving a grandchild on stdout (a wrapper script that forks instead of exec'ing — what `/bin/sh` does on Linux, where it is dash) made `cmd.Output()` block for the grandchild's full lifetime regardless of the context. Keep `WaitDelay` set on any similar short captured-output exec. `lstk az` needs the same treatment but a different protocol (argcomplete: `_ARGCOMPLETE=1`, output on fd 8).

# CLI Help Text

- Write command `Short`/`Long` as unbroken paragraphs (one line each, blank line between); never hard-wrap a sentence in source. `wrapText` in `cmd/help.go` re-wraps to the terminal width at render time and `lstk docs` reads the raw text, so manual breaks fight both. Indented lines (examples, aligned output) are left as-is.

# Writing for Humans

When drafting Slack messages, PR descriptions, review replies, release notes, or README text: keep it short and plain, lead with the point, and produce one tight draft rather than multiple options.

# Testing

- **TDD is mandatory for every feature and bug fix (red → green → refactor).** First write an end-to-end integration test that reproduces the bug or specifies the new behavior, and run it to confirm it fails for the expected reason. Only then implement the change, and run the test again to confirm it passes. Never write the implementation first and backfill the test.
- E2E tests must assert only **observable behavior** — what a user sees through the CLI (exit codes, output, files written, requests the emulator/wrapped tool receives) — never internal details (function calls, internal state, log internals). If it's unclear what the observable behavior is, ask the user before writing the test.
- To keep e2e tests faithful, it's fine (and encouraged) to provide observable mocks of *external* systems — a mock auth/license endpoint, a fake `aws`/`az` binary on `PATH`, a fake browser opener. These mocks define the boundary between lstk and the outside world; do not mock lstk's own internals in e2e tests. Existing examples: `fakeBrowserOpener` in `test/integration/login_test.go`, mock platform-API servers.
- Prefer integration tests to cover most cases. Use unit tests when integration tests are not practical.
- Assertions use `github.com/stretchr/testify` (`require` for fatal checks). Snapshots use the in-house `internal/snap` helper: `snap.Match(t, s)` stores snapshots in one archive per test file (`__snapshots__/<test_file>.snap`): a `[TestName_N]` header line per Match call, the value verbatim, and a `---` terminator on its own line (so a value and the same value plus one final newline are indistinguishable; other newlines are preserved exactly). Missing snapshots are created locally but fail in CI; `UPDATE_SNAPS=true go test` rewrites them. The format has no escaping, so a value containing a line that is exactly `---` is rejected (round-trip guard) — sanitize such values before matching. `snap.MatchJSON(t, raw, "data.currentVersion", ...)` snapshots a JSON document in canonical pretty-printed form, masking the values at the given dotted paths with `<any>` (a path that doesn't resolve fails the test; objects only, no array indexing). A package using snapshots must wire `func TestMain(m *testing.M) { os.Exit(snap.Clean(m)) }` — obsolete snapshots then fail the run (or are deleted under `UPDATE_SNAPS=true`); cleanup is skipped on filtered (`-run`/`-skip`) or failed runs. Sanitize volatile values before matching with label-anchored regexes, not value-shaped ones (see `sanitizeSnapshot` in `internal/output`; RE2 has no lookahead, so a bare semver pattern corrupts IPv4 strings).
- **When fixing a bug, always add an integration test** that fails before the fix and passes after. This prevents regressions and documents the exact scenario that was broken.
- Integration tests that run the CLI binary with Bubble Tea must use a PTY since Bubble Tea requires a terminal. Use the cross-platform helpers in `test/integration/pty_helpers_test.go` — `runLstkInPTY` for run-to-completion, or `startLstkInPTY`/`startCmdInPTY` returning a `ptyProc` with `waitForOutput`/`write`/`wait`/`kill` for interactive prompts — never `creack/pty` directly. The helpers wrap `charmbracelet/x/xpty` (Unix PTY on macOS/Linux, ConPTY on Windows), so PTY tests run on Windows too; output from `wait()`/`output()` is ANSI-stripped because ConPTY injects its own repaint sequences. `creack/pty` remains only in `signal_forwarding_test.go` (build-tagged `!windows`; POSIX signal semantics don't port).
- Never fake an external CLI (`aws`, `az`, `terraform`, `cdk`, `sam`, `aws_completer`, browser openers) with a shell script — scripts don't run on Windows. Use `writeFakeTool`/`installFakeTool` (`test/integration/faketool_test.go`), which copy a compiled Go stand-in (`test-samples/faketool`) onto PATH configured via a JSON sidecar (echo lines with `{argN}`/`{args}`/`{env:NAME}` placeholders, arg-prefix cases like `--version`, sleep, exit codes, record-to-file).
- Windows CI runs the full integration suite minus tests that are inherently unavailable there: Docker-backed tests (`requireDocker` skips on Windows CI — no nested virtualization), unix-socket tests, POSIX signal/`os.Interrupt` tests, bash-completion, symlink-permission tests, and the two inner-PTY streaming tests (`proc.RunInPTY` is unix-only). Everything else — PTY prompts included — must run on Windows; don't add `runtime.GOOS == "windows"` skips for convenience.
- For unreachable-Docker setups use `DOCKER_HOST=tcp://localhost:1` (see `unreachableDockerHost`), not a unix socket path, so the test stays valid on Windows.
- Mark every integration test with `t.Parallel()` unless it shares external state with other tests. Today the main blocker is the Docker daemon: tests that start LocalStack containers cannot run concurrently because lstk's container discovery matches by `(image, internal port)`, so two parallel runs would cross-contaminate. Tests that only touch the filesystem, mock servers, or the CLI binary itself should be parallel.
- Never let an integration test inherit the developer's real `$HOME`. Pass an isolated env via `testEnvWithHome(t.TempDir(), "")` (or build on top of it with `env.With(...)`) instead of `nil` or `os.Environ()`. Inheriting HOME pollutes the user's `~/.config/lstk/`, `~/.aws/`, and `~/.cache/lstk/`, and makes parallel runs interfere through shared `lstk.log`, license cache, and file-keyring fallback.
- Never let a login test open a real browser tab. `internal/auth.New` accepts `auth.WithBrowserOpener(open func(string) error)` to override how the login flow opens the auth URL — unit/TUI tests inject a recorder (see `internal/ui/run_login_test.go`'s `browserRecorder`) and assert on the captured URL instead of the real one. CLI-binary integration tests instead prepend a temp dir with fake `open`/`xdg-open`/`x-www-browser`/`www-browser` scripts onto `PATH` (see `fakeBrowserOpener` in `test/integration/login_test.go`), since `github.com/pkg/browser` shells out to whichever of those exists.

# Output Routing and Events

- Emit typed events via `sink.Emit(output.XxxEvent{...})` instead of printing from domain/command handlers. For simple messages use `output.MessageEvent{Severity: output.SeverityInfo, Text: "..."}` (severities: `SeverityInfo`, `SeveritySuccess`, `SeverityNote`, `SeverityWarning`, `SeveritySecondary`).
- User-facing failures are output too: surface them with `sink.Emit(output.ErrorEvent{Title, Summary, Actions})` (use `Actions` for actionable next-steps, e.g. a command to run), then return `output.NewSilentError(err)`. The top-level handler in `cmd/root.go` checks `output.IsSilent(err)` and skips re-printing silent errors; any non-silent error returned up the stack falls through to an unstyled `Error: %v` on stderr — that is the fallback for when no sink is available (e.g. logger setup), not the preferred path for a failure a sink could have rendered.
- Sink implementations belong in `internal/output`; do not implement `output.Sink` outside that package.
- Reuse `FormatEventLine(event Event)` for all line-oriented rendering so plain and TUI output stay consistent.
- Select output mode at the command boundary in `cmd/`: interactive TTY runs Bubble Tea, non-interactive mode uses `output.NewPlainSink(...)`.
- Keep non-TTY mode non-interactive (no stdin prompts or input waits).
- Domain packages (`internal/` minus `internal/ui/`) must not import Bubble Tea or UI packages. A useful test: domain code should work unchanged if `internal/ui/` were swapped for a different frontend.
- Any feature/workflow package that produces user-visible progress should accept an `output.Sink` dependency and emit events through `internal/output`.
- Do not pass UI callbacks like `onProgress func(...)` through domain layers; prefer typed output events.
- Event payloads should be domain facts (phase/status/progress), not pre-rendered UI strings.
- When adding a new event type, update all of:
  - `internal/output/events.go` (event struct definition)
  - `internal/output/plain_format.go` (line formatting fallback)
  - tests in `internal/output/*_test.go` for formatter/sink behavior parity

## Structured output (`--json`)

A JSON-capable command emits a single `output.Envelope` (schema version, `data`/`error` discriminated on `status`, an enumerated `error.code`) instead of formatted lines — see [docs/structured-output.md](docs/structured-output.md) for the full envelope contract, error-code table, exit-code conventions, and the per-command catalog (implemented vs. planned). `output.EnvelopeSink` builds the envelope from the same event vocabulary described above; adding `--json` support to a command is documented step by step in that file's "Adding `--json` support to a command" section. Command opt-in is explicit via the `jsonSupportedAnnotation` on the `cobra.Command` in `cmd/`.

## User Input Handling

Domain code must never read from stdin or wait for user input directly. Instead:

1. Emit a `UserInputRequestEvent` via `sink.Emit(output.UserInputRequestEvent{...})` with:
   - `Prompt`: message to display
   - `Options`: available choices (e.g., `{Key: "enter", Label: "Press ENTER to continue"}`)
   - `ResponseCh`: channel to receive the user's response

2. Wait on the `ResponseCh` for an `InputResponse` containing:
   - `SelectedKey`: which option was selected
   - `Cancelled`: true if user cancelled (e.g., Ctrl+C)

3. The TUI (`internal/ui/app.go`) handles these events by showing the prompt and sending the response when the user interacts. `internal/ui/` is responsible only for the interaction itself — it does not contain the logic that acts on the response.

4. The logic executed in response to the user's choice (e.g., writing config, starting a container) belongs in a domain package alongside the rest of the feature, not in `internal/ui/`.

5. In non-interactive mode, commands requiring user input should fail early with a helpful error (e.g., "set LOCALSTACK_AUTH_TOKEN or run in interactive mode").

Example flow in auth login:
```go
responseCh := make(chan output.InputResponse, 1)
sink.Emit(output.UserInputRequestEvent{
    Prompt:     "Waiting for authentication...",
    Options:    []output.InputOption{{Key: "enter", Label: "Press ENTER when complete"}},
    ResponseCh: responseCh,
})

select {
case resp := <-responseCh:
    if resp.Cancelled {
        return "", context.Canceled
    }
    // proceed with user's choice
case <-ctx.Done():
    return "", ctx.Err()
}
```

# UI Development (Bubble Tea TUI)

## Structure
- `internal/ui/` - Bubble Tea app model and run orchestration
- `internal/ui/components/` - Reusable presentational components
- `internal/ui/styles/` - Lipgloss style definitions and palette constants

## Component and Model Rules
1. Keep components small and focused (single concern each).
2. Keep UI as presentation/orchestration only; business logic stays in domain packages.
3. Long-running work must run outside `Update()` (goroutine or command path), with UI updates sent asynchronously.
4. Bubble Tea updates from background work should flow through `Program.Send()` via `output.NewTUISink(...)`.
5. `Update()` must stay non-blocking.
6. UI should consume shared output events directly; add UI-only wrapper/control messages only when needed, and suffix them with `...Msg`.
7. Keep message/history state bounded (for example, capped line buffer).
8. A pending `UserInputRequestEvent` must stay visible and readable until it is answered: show it through `inputPrompt` (spinner text is a mirror, never the only home — a spinner stopped inside its min duration erases it) and wrap it to the terminal width (Bubble Tea truncates every line, cutting off the key hint). Either failure leaves the CLI blocked on `ResponseCh` with nothing on screen.

## Styling Rules
- Define styles with semantic names in `internal/ui/styles/styles.go`.
- Preserve the Nimbo palette constants (`#3F51C7`, `#5E6AD2`, `#7E88EC`) unless intentionally changing branding.
- If changing palette constants, update/add tests to guard against accidental drift.

# Spec-Driven Changes

`openspec/` holds specs and change proposals (`openspec/specs/`, `openspec/changes/`, archive under `openspec/changes/archive/`); change IDs referenced elsewhere in this file (e.g. `add-bundled-extension-distribution`) live there. Background: `docs/spec-driven-development.md`.

# Claude Skills

Custom skills are available in `.claude/skills/`:

- `/add-command <name>` — Scaffold a new CLI subcommand with proper cmd/ wiring, domain logic, sink handling, and tests
- `/add-event <EventName>` — Add a new output event type to the event/sink system with format parity
- `/add-component <name>` — Scaffold a new Bubble Tea TUI component
- `/review-pr <number>` — Review a PR against architectural patterns
- `/create-pr` — Create a PR with conventional format and Linear ticket linking

# Maintaining This File

When making significant changes to the codebase (new commands, architectural changes, build process updates, new patterns), update this CLAUDE.md file to reflect them — but only when the guidance spans packages. Anything specific to a single declaration — a function, type, method, struct field, or constant — belongs in a doc comment on that declaration, not here.

**This is the only agent-instruction file in the repo.** Where each kind of detail should go instead:

- **Anything about a single declaration** (mechanism, rationale, invariants, why an obvious alternative was rejected, upstream/external behaviour it depends on) → a doc comment on that function, type, method, field, or constant. Negative statements work fine there too: anchor "there is deliberately no X" to the function where X would have gone.
- **User-facing config reference** → `internal/config/default_config.toml`, which ships as the user's own commented config.
- **User-facing command reference** → the command's Cobra `Short`/`Long` in `cmd/`, which is also what `lstk docs` renders.
- **Design rationale and non-goals for in-flight work** → `openspec/changes/<id>/design.md`.
- **Why a change was made** → the commit message and the Linear ticket. Ticket IDs in code comments (the existing DEVX-658 / DEVX-984 / PRO-324 pattern) carry provenance to the reader who needs it.
