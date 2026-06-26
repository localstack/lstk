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
make test-integration   # Run integration tests (builds first, requires Docker)
make lint               # Run golangci-lint
make mock-generate      # Run go generate to regenerate mocks
make clean              # Remove build artifacts
```

Run a single integration test:
```bash
make test-integration RUN=TestStartCommandSucceedsWithValidToken
```

Note: Integration tests require `LOCALSTACK_AUTH_TOKEN` environment variable for valid token tests.

# Architecture

- `main.go` - Entry point
- `cmd/` - CLI wiring only (Cobra framework), no business logic
- `internal/` - All business logic goes here
  - `container/` - Handling different emulator containers
  - `runtime/` - Abstraction for container runtimes (Docker, Kubernetes, etc.) - currently only Docker implemented
  - `auth/` - Authentication (env var token or browser-based login)
  - `config/` - Viper-based TOML config loading and path resolution
  - `output/` - Generic event and sink abstractions for CLI/TUI/non-interactive rendering
  - `ui/` - Bubble Tea views for interactive output
  - `update/` - Self-update logic: version check via GitHub API, binary/Homebrew/npm update paths, archive extraction
  - `log/` - Internal diagnostic logging (not for user-facing output — use `output/` for that)
  - `iac/` - Wrappers for third-party infrastructure as code tools (Terraform, AWS CDK, AWS SAM CLI).

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
When adding a new command that depends on configuration, wire config initialization explicitly in that command (`PreRunE: initConfig`). Keep side-effect-free commands (e.g., `version`, `config path`) without config initialization.

Created automatically on first run with defaults. Supports emulator types: `aws`, `snowflake`, and `azure`.

Each `[[containers]]` block may set an optional `image` to override the default Docker Hub image (e.g. an internal registry mirror or a locally loaded offline image). `ContainerConfig.Image()` returns `image` as-is when it already carries a tag (so the separately-configured `tag` is dropped in that case), otherwise it appends `tag` (or `latest`); the default `localstack/<product>:<tag>` is used when `image` is unset.

## Volume Mounts

Each `[[containers]]` block accepts a `volumes` list of Docker-style `"host:container[:ro]"` bind specs (e.g. for Snowflake init hooks mounted into `/etc/localstack/init/{boot,start,ready,shutdown}.d`). The persistence/cache mount to `/var/lib/localstack` is folded into this list: the entry whose container target is `/var/lib/localstack` (`persistenceTarget` in `internal/config/containers.go`) defines the host dir backing it, and that path is what `VolumeDir()`, `lstk volume path`, and `lstk volume clear` resolve. Resolution precedence in `VolumeDir()`: a `volumes` entry targeting `/var/lib/localstack` → the legacy singular `volume = "..."` field (still honored for backward compatibility) → the default OS cache dir. Setting the persistence dir via both `volume` and a `volumes` entry with differing sources is a validation error.

`volume` (singular) and `volumes` (plural) are not interchangeable in general — they overlap only for the persistence mount. `volume` *only* sets the persistence dir (always mounted to `/var/lib/localstack`); `volumes` is a superset that can set the persistence dir **and** arbitrary mounts. Two further distinctions: `volume` cannot express init hooks or any non-persistence mount, and the legacy `volume` value is used **verbatim** (no path resolution) whereas a `volumes` source is resolved. So `volume = "/data"` and `volumes = ["/data:/var/lib/localstack"]` are equivalent for persistence, but `volume = "./data"` is passed raw (and would become a Docker named volume) while `volumes = ["./data:/var/lib/localstack"]` resolves `./data` against the config dir.

Parsing/resolution lives in `parseVolume`/`ExtraVolumes` in `internal/config/containers.go`. The container target is validated with `path.IsAbs` (slash semantics) — never `filepath.IsAbs`, which rejects `/var/lib/localstack` on Windows. `splitVolumeSpec` rejoins a leading Windows drive letter (`C:\…`) onto the host source so its `:` is not mistaken for the host/container separator (Windows-guarded so a single-letter relative dir like `a:/data` stays valid elsewhere, matching Docker). Relative host sources resolve against the **config file's directory** and a leading `~/` is expanded — this is required because the Docker SDK treats a non-absolute source as a *named volume* rather than a bind mount. `start.go` mounts the persistence dir (creating it via `MkdirAll`) and appends `ExtraVolumes()`; extra sources are not created (`os.Stat` + error if missing) since init-hook entries are files, not dirs.

# Offline / Enterprise Environments

There is no `--offline` flag. Instead `container.Start` degrades gracefully when internet requests fail (the common enterprise blockers: Docker Hub unreachable, proxy/TLS interception, license server unreachable):

- **Image pull**: if `rt.PullImage` fails but `rt.ImageExists` reports the image is already present locally, lstk warns and uses the local image instead of failing.
- **License pre-flight (image already local)**: when a pinned image is already present locally — so `pullImages` won't pull it — `tryPrePullLicenseValidation` skips the pre-flight check entirely (gated on `rt.ImageExists`), since the redundant network round-trip would otherwise block a fully-offline start; the container validates its own bundled license at startup. This is symmetric with the skip-pull behaviour above.
- **License pre-flight (server unreachable)**: when a check does run, `validateLicense` distinguishes a definitive server rejection (`*api.LicenseError`, e.g. HTTP 403/400 — still fatal) from a transport-level failure (any other error — offline/proxy/cert). On a transport failure it skips the pre-flight check and lets the container validate its own bundled license at startup.
- **Telemetry/update checks** are already best-effort and fail silently when offline.

`runtime.PullImage` always closes its `progress` channel (even when `ImagePull` fails early) so the local-image fallback path doesn't leak the progress goroutine. Pair this with a custom `image` in the config to point at a locally loaded image or an internal-registry mirror.

# Emulator Setup Commands

Use `lstk setup <emulator>` to set up CLI integration for an emulator type:
- `lstk setup aws` — Sets up AWS CLI profile in `~/.aws/config` and `~/.aws/credentials`
- `lstk setup azure` — Prepares an isolated Azure CLI config dir (under the lstk config dir, via `AZURE_CONFIG_DIR`): registers a custom Azure cloud (`LocalStack`) whose endpoints point at the LocalStack Azure emulator, activates it, disables Azure CLI instance discovery and telemetry, and performs a one-time dummy service-principal login. The user's global `~/.azure` is left untouched. Requires the `az` CLI and a running Azure emulator.
- `lstk az <args>` — Runs `az <args>` against that isolated config dir, so the Azure CLI talks to LocalStack for Azure service URLs and to the real internet for everything else (extension downloads, etc.).
- `lstk az start-interception` / `lstk az stop-interception` — Opt-in second mode: instead of the isolated dir, these mutate the user's **global** `~/.azure` so plain `az` (any terminal/script) targets LocalStack, then switch back. `start-interception` runs the same register → activate → `instance_discovery=false` → dummy-login flow against the global config (but does not touch global telemetry/survey prefs) and is independent of `lstk setup azure`. `stop-interception` switches the active cloud back to `AzureCloud` (override with `--cloud <name>`, validated against the live `az cloud list`) and re-enables instance discovery — but only if `LocalStack` is still the active cloud, to avoid clobbering an unrelated selection.

This naming avoids AWS-specific "profile" terminology and uses a clear verb for mutation operations.
The deprecated `lstk config profile` command still works but points users to `lstk setup aws`.

The default `lstk az <args>` mode mirrors `lstk aws`: the Azure CLI has no `--endpoint-url`/`--profile`, so the only isolation knob is `AZURE_CONFIG_DIR`. Inside that isolated dir we register a custom cloud whose endpoints point at `https://azure.localhost.localstack.cloud:4566`, so `az` makes direct calls to LocalStack for Azure services (no HTTP(S) forward proxy in front of `az`). `core.instance_discovery=false` is required because `az` does not recognise the LocalStack host as a real Azure cloud. Adding a new Azure service that needs its own endpoint in `az`'s cloud config means extending the map in `internal/azureconfig/azureconfig.go::BuildCloudConfig`.

`lstk az start-interception`/`stop-interception` additionally offer azlocal's global pattern (the same cloud registration applied to `~/.azure` rather than the isolated dir), so existing `az` scripts run unmodified against LocalStack. This is intentionally documented as optional because it mutates global state; prefer the isolated `lstk az <args>` mode unless a script must invoke plain `az`. The interception domain logic lives in `internal/azureconfig/interception.go` and reuses the shared `registerLocalStackCloud` helper; the command wiring (subcommands under `az` plus the shared `azPreflight` checks) is in `cmd/az.go`.

Environment variables:
- `LOCALSTACK_AUTH_TOKEN` - Auth token (skips browser login if set)
- `LSTK_OTEL=1` - Enables OpenTelemetry trace export (disabled by default); when enabled, standard `OTEL_EXPORTER_OTLP_*` env vars are respected by the SDK. Requires an OTLP-compatible backend to receive and visualize telemetry — for local development, `make otel` starts one (UI at http://localhost:16686).

# Infrastructure as Code Commands

lstk proxies third-party IaC tools at the AWS emulator so they run against LocalStack with no `*local` wrapper installed. Each command forwards its args to the real tool after configuring the environment; domain logic lives under `internal/iac/<tool>/cli/`, wiring in `cmd/<tool>.go`, with shared command-boundary helpers in `cmd/iac.go`. Siblings: `lstk terraform` (alias `tf`), `lstk cdk`, `lstk sam`.


# Snapshots

`lstk snapshot` captures and restores the running emulator's state. For Snowflake and Azure, snapshot support is still maturing, so these commands surface a friendly heads-up that results may be incomplete. Domain logic lives in `internal/snapshot/`; `cmd/snapshot.go` is wiring + output-mode selection.

- `lstk snapshot save [destination]` — export state to a local `.snapshot` file or a named cloud snapshot.
- `lstk snapshot load REF` — restore state, starting the emulator first if needed; `--merge` controls how snapshot state combines with running state (`account-region-merge` (default), `overwrite`, `service-merge`).
- `lstk snapshot list` — list cloud snapshots on the LocalStack platform. Lists only snapshots you created by default; pass `--all` to include every snapshot in your organization. Cloud-only; requires auth.
- `lstk snapshot remove REF` — delete a cloud snapshot. Cloud-only; local files are never deleted by the CLI. Prompts for confirmation in interactive mode; `--force` is required to skip the prompt in non-interactive mode.
- `lstk snapshot show REF` — show metadata for a single cloud snapshot (name, created date, size, LocalStack version, message, services, and per-service resource counts). Resource counts render only when the platform has them for that snapshot. Cloud-only; requires auth.

A REF is parsed by helpers in `internal/snapshot/destination.go`:
- **local file** — absolute/relative path; the `.snapshot` extension is forced (any other extension is replaced). On load, `.zip` files saved by older lstk versions are still accepted.
- **cloud snapshot** — `pod:` prefix (e.g. `pod:my-baseline`), stored on the LocalStack platform. Requires auth (`LOCALSTACK_AUTH_TOKEN` or `lstk login`).
- **S3 remote** — `s3://bucket/prefix` (parsed to `KindS3`). The CLI never touches S3; the emulator performs the transfer. See the S3 remotes note below.

`ParseDestination` (save), `ParseSource` (load), `ParseRemovable` (remove), and `ParseShowable` (show) share pod-name validation; `ParseRemovable` and `ParseShowable` reject local paths (via the shared `parseCloudOnly` helper) so those cloud-only commands never touch local files.

**S3 remotes (save/load/list only).** `lstk snapshot save <pod-name> s3://bucket/prefix`, `load <pod-name> s3://bucket/prefix`, and `list s3://bucket/prefix` store snapshots in the user's own S3 bucket. The pod name (the snapshot's identity within the bucket) is a positional separate from the `s3://` location — required for load, auto-generated for save when omitted, unused for list. Credentials come from `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` (optional `AWS_SESSION_TOKEN`) or `--profile <name>` (read via `internal/awsconfig.ReadProfileCredentials`/`CredentialsFromEnv`); **never put credentials in the URL** (rejected by `parseS3`). The emulator-side S3 remote requires a remote to be registered by name first, so the CLI transparently upserts a deterministic remote (`POST /_localstack/pods/remotes/<name>`, name derived in `internal/snapshot/remote.go`) with a placeholder-templated URL, then passes the real credentials as ephemeral `remote_params` on each op — secrets stay out of the registered URL and out of any persisted state. `list s3://…` queries the emulator (`GET /_localstack/pods` with a remote body), not the platform API, so it **requires a running emulator** (unlike `snapshot list`). Before save/load/list, lstk runs a pre-flight bucket-existence check (`ensureBucketExists` → `RemoteClient.S3BucketExists`, an unsigned S3 `HEAD`: 404 ⇒ missing) and errors out rather than letting the emulator auto-create a bucket on a typo; local-testing endpoints (IP / `host.docker.internal`) are skipped, and a check that can't run degrades to a warning. Domain logic + client interface live in `internal/snapshot/remote.go`; the emulator HTTP impl is `internal/emulator/aws/remote.go`; command wiring/arg classification (`classifyRemoteArgs`, `resolveS3Credentials`) is in `cmd/snapshot.go`. ORAS and other remotes, and `remove`/`show`/versions on S3, are not yet supported.

**Auto-load on start.** A `[[containers]]` block (AWS only) can set `snapshot = "pod:my-baseline"` (any load REF) to auto-load that snapshot after the emulator starts. The loader runs only when the emulator is freshly started this run (skipped when already running), mirroring v1's `AUTO_LOAD_POD`. `lstk start --snapshot REF` overrides the configured REF for one run; `lstk start --no-snapshot` skips it. Resolution lives in `resolveStartSnapshotRef`/`newSnapshotAutoLoader` in `cmd/snapshot.go`; the loader is threaded into the non-interactive start in `cmd/root.go` and into the TUI via `ui.RunOptions.PostStart`. `snapshot save` never writes back into config — the `snapshot` field is manual.

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
- Do not call `config.Get()` from domain/business-logic packages. Instead, extract the values you need at the command boundary (`cmd/`) and pass them as explicit function arguments. This keeps domain functions testable without requiring Viper/config initialization.

# CLI Help Text

- Write command `Short`/`Long` as unbroken paragraphs (one line each, blank line between); never hard-wrap a sentence in source. `wrapText` in `cmd/help.go` re-wraps to the terminal width at render time and `lstk docs` reads the raw text, so manual breaks fight both. Indented lines (examples, aligned output) are left as-is.

# Testing

- Prefer integration tests to cover most cases. Use unit tests when integration tests are not practical.
- **When fixing a bug, always add an integration test** that fails before the fix and passes after. This prevents regressions and documents the exact scenario that was broken.
- Integration tests that run the CLI binary with Bubble Tea must use a PTY (`github.com/creack/pty`) since Bubble Tea requires a terminal. Use `pty.Start(cmd)` instead of `cmd.CombinedOutput()`, read output with `io.Copy()`, and send keystrokes by writing to the PTY (e.g., `ptmx.Write([]byte("\r"))` for Enter).
- Mark every integration test with `t.Parallel()` unless it shares external state with other tests. Today the main blocker is the Docker daemon: tests that start LocalStack containers cannot run concurrently because lstk's container discovery matches by `(image, internal port)`, so two parallel runs would cross-contaminate. Tests that only touch the filesystem, mock servers, or the CLI binary itself should be parallel.
- Never let an integration test inherit the developer's real `$HOME`. Pass an isolated env via `testEnvWithHome(t.TempDir(), "")` (or build on top of it with `env.With(...)`) instead of `nil` or `os.Environ()`. Inheriting HOME pollutes the user's `~/.config/lstk/`, `~/.aws/`, and `~/.cache/lstk/`, and makes parallel runs interfere through shared `lstk.log`, license cache, and file-keyring fallback.

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

## Styling Rules
- Define styles with semantic names in `internal/ui/styles/styles.go`.
- Preserve the Nimbo palette constants (`#3F51C7`, `#5E6AD2`, `#7E88EC`) unless intentionally changing branding.
- If changing palette constants, update/add tests to guard against accidental drift.

# Claude Skills

Custom skills are available in `.claude/skills/`:

- `/add-command <name>` — Scaffold a new CLI subcommand with proper cmd/ wiring, domain logic, sink handling, and tests
- `/add-event <EventName>` — Add a new output event type to the event/sink system with format parity
- `/add-component <name>` — Scaffold a new Bubble Tea TUI component
- `/review-pr <number>` — Review a PR against architectural patterns
- `/create-pr` — Create a PR with conventional format and Linear ticket linking

# Maintaining This File

When making significant changes to the codebase (new commands, architectural changes, build process updates, new patterns), update this CLAUDE.md file to reflect them.
