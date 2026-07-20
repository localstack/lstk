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
make build              # Compiles to bin/lstk (cleans first)
make test               # Run unit tests (cmd/ and internal/) via gotestsum
make test-integration   # Run integration tests (rebuilds bin/lstk via `build`, requires Docker)
make lint               # Run golangci-lint (version pinned via .tool-versions)
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
- Mocks are generated with mockgen (go.uber.org/mock) via per-file `//go:generate mockgen ...` directives (e.g. `internal/snapshot/remote.go`); adding a mock means adding a directive, then `make mock-generate`.
- Set `CREATE_JUNIT_REPORT=1` to get a JUnit XML report from `make test` / `make test-integration`.

# Architecture

- `main.go` - Entry point
- `cmd/` - CLI wiring only (Cobra framework), no business logic; one file per command
- `internal/` - All business logic goes here
  - `api/` - LocalStack platform API client (auth, license)
  - `auth/` - Authentication (env var token or browser-based login), token storage/keyring
  - `awscli/`, `azurecli/` - Exec wrappers behind the `lstk aws` / `lstk az` proxy commands
  - `awsconfig/` - AWS CLI profile management in `~/.aws/` (`lstk setup aws`)
  - `azureconfig/` - Azure CLI cloud registration and interception (`lstk setup azure`, `lstk az`) — see `internal/azureconfig/CLAUDE.md`
  - `caller/` - Classifies the invoking caller/harness (human vs agent) for telemetry
  - `config/` - Viper-based TOML config loading and path resolution — see `internal/config/CLAUDE.md`
  - `container/` - Handling different emulator containers (start flow, gateway ports, offline fallbacks) — see `internal/container/CLAUDE.md`
  - `emulator/` - Emulator API abstraction with per-type implementations (`aws/`, `azure/`, `snowflake/`)
  - `endpoint/` - Emulator endpoint/host resolution
  - `env/` - Process environment snapshot/injection helper (also used to isolate test envs)
  - `extension/` - Git-style `lstk-<name>` extension resolution and exec
  - `iac/` - Wrappers for third-party infrastructure as code tools (`terraform/`, `cdk/`, `sam/`)
  - `log/` - Internal diagnostic logging (not for user-facing output — use `output/` for that)
  - `output/` - Generic event and sink abstractions for CLI/TUI/non-interactive rendering
  - `ports/` - Port availability checks
  - `reset/` - `lstk reset` domain logic
  - `runtime/` - Abstraction for container runtimes (Docker, Kubernetes, etc.) - currently only Docker implemented
  - `snapshot/` - Snapshot save/load/list/remove/show domain logic — see `internal/snapshot/CLAUDE.md`
  - `telemetry/` - CLI analytics events client
  - `terminal/` - Plain-mode terminal helpers (spinner, TTY detection)
  - `tracing/` - OpenTelemetry setup (`LSTK_OTEL=1`)
  - `ui/` - Bubble Tea views for interactive output
  - `update/` - Self-update logic: version check via GitHub API, binary/Homebrew/npm update paths, archive extraction
  - `version/` - Version info
  - `volume/` - `lstk volume` domain logic

Commands are registered in `cmd/root.go` in two Cobra groups: the `commands` group (start, stop, restart, login, logout, status, logs, setup, config, volume, update, docs, snapshot, reset, save, load) and the `tools` group of proxy commands (aws, terraform/tf, cdk, sam, az). Shared helpers: `cmd/root.go` (wiring, groups, `requireSubcommand`, `initConfig`), `cmd/help.go` (help template), `cmd/iac.go` (IaC command boundary), `cmd/extension.go` (extension dispatch).

# Commits, PRs, and Linear

- Commit messages: a single concise line. Never add `Co-Authored-By: Claude`, "Generated with Claude Code", or any other AI attribution to commits or PR bodies.
- Never commit or push unless explicitly asked.
- PRs are squash-merged; titles start with an action verb and stay under ~70 characters.
- Every PR needs exactly one `semver:` label (`patch`/`minor`/`major`) and one `docs:` label (`skip`/`needed`) — enforced by `check-release-label.yml`. Use `/create-pr` to scaffold title, body, and labels.
- Issues and tickets live in Linear, not GitHub Issues. Typical flow: Linear issue → branch named from the issue (e.g. `devx-123-...`) → PR body ends with `Closes DEVX-123` (or `Towards DEVX-123` if partial). Ask which Linear team if unclear (e.g. PRO = product, DEVX = developer experience).

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
When adding a new command that depends on configuration, wire config initialization explicitly in that command (`PreRunE: initConfig(nil)`; the root command uses `initConfigDeferCreate(&firstRun)`). Keep side-effect-free commands (e.g., `version`, `config path`) without config initialization.

A parent command that only groups subcommands (e.g. `config`, `setup`, `volume`, `snapshot`) must call `requireSubcommand(cmd)` (in `cmd/root.go`). Cobra otherwise prints help and exits 0 for an unknown/missing subcommand of a non-runnable parent; `requireSubcommand` sets `cobra.NoArgs` plus a help-printing `RunE` so a bare invocation still shows help (exit 0) while an unknown subcommand exits non-zero. Cobra's autogenerated `completion` command is the same shape, but it is created lazily during `Execute`, so `NewRootCmd` calls `root.InitDefaultCompletionCmd()` to materialize it before applying `requireSubcommand` (the call is idempotent — Cobra skips re-adding it).

Created automatically on first run with defaults. Supports emulator types: `aws`, `snowflake`, and `azure`.

Only one `[[containers]]` block may be enabled at a time. `container.Start` rejects a config with more than one block up front (before health/auth checks and image pulls), since running multiple emulators together (e.g. AWS + Snowflake) is unsupported and would otherwise fail later during startup with container-name conflicts or port collisions. The guard lives on the start path (not `config.Get()`) on purpose: recovery/reporting commands like `stop`, `status`, and `logout` must still enumerate multiple running emulators.

Each `[[containers]]` block may set an optional `image` (override the default Docker Hub image) and a `volumes` list of Docker-style bind specs (persistence dir, init hooks, arbitrary mounts). Image/tag precedence, `volume` vs `volumes` semantics, and path-resolution rules are documented in `internal/config/CLAUDE.md`.

`GATEWAY_LISTEN` (host exposure and published ports) is read from the container's resolved env, not hardcoded; parsing and derivation are documented in `internal/container/CLAUDE.md`.

# Offline / Enterprise Environments

There is no `--offline` flag. Instead `container.Start` degrades gracefully when internet requests fail (Docker Hub unreachable, proxy/TLS interception, license server unreachable): local images are used when pulls fail, and the license pre-flight is skipped on transport-level failures or unsupported-tag rejections so the container validates its own bundled license. The exact fallback rules live in `internal/container/CLAUDE.md`; pair them with a custom `image` in the config to point at a locally loaded image or an internal-registry mirror.

# Emulator Setup Commands

Use `lstk setup <emulator>` to set up CLI integration for an emulator type:
- `lstk setup aws` — Sets up an AWS CLI `localstack` profile in `~/.aws/config` and `~/.aws/credentials`. Runs interactively (Y/n prompt) on a TTY; in non-interactive mode (CI / piped / `--non-interactive`) it writes the profile with defaults and exits 0 without prompting, and returns write/check failures as errors so automation exits non-zero. Overwriting an existing `localstack` profile whose values differ requires `--force`. Shared host resolution lives in `awsconfig.ResolveProfileHost`; the non-interactive write is `awsconfig.SetupNonInteractive`, the interactive path is `awsconfig.Setup(..., skipConfirm)`.
- `lstk setup azure` (alias `lstk setup az`) — Prepares an isolated Azure CLI config dir pointing at the LocalStack Azure emulator; the user's global `~/.azure` is untouched. `lstk az <args>` then runs `az` against that isolated dir. `lstk az start-interception` / `stop-interception` are the opt-in global mode that mutates `~/.azure` so plain `az` targets LocalStack. Mechanics, rationale, and extension points: `internal/azureconfig/CLAUDE.md`.

This naming avoids AWS-specific "profile" terminology and uses a clear verb for mutation operations.

Environment variables:
- `LOCALSTACK_AUTH_TOKEN` - Auth token (skips browser login if set)
- `LSTK_STARTUP_TIMEOUT` - Startup readiness deadline for `lstk start` (Go duration). Zero/unset uses the per-mode default resolved in `resolveStartupTimeout` (`internal/container/start.go`): 20s interactive (deadline only shows a recoverable keep-waiting/stop prompt, re-armed by "keep waiting"), 60s non-interactive (fatal; the container is left running for inspection). Container exits are detected separately — and instantly, with the exit code — via the exit wait `runtime.Runtime.Start` registers between create and start.
- `LSTK_OTEL=1` - Enables OpenTelemetry trace export (disabled by default); when enabled, standard `OTEL_EXPORTER_OTLP_*` env vars are respected by the SDK. Requires an OTLP-compatible backend to receive and visualize telemetry — for local development, `make otel` starts one (UI at http://localhost:16686).

# Infrastructure as Code Commands

lstk proxies third-party IaC tools at the AWS emulator so they run against LocalStack with no `*local` wrapper installed. Each command forwards its args to the real tool after configuring the environment; domain logic lives under `internal/iac/<tool>/cli/`, wiring in `cmd/<tool>.go`, with shared command-boundary helpers in `cmd/iac.go`. Siblings: `lstk terraform` (alias `tf`), `lstk cdk`, `lstk sam`.

# Extensions

lstk supports Git-style extensions: when `lstk <name>` is not a built-in command or alias, lstk resolves and execs an external `lstk-<name>` executable, forwarding arguments verbatim and propagating the exit code. Built-ins always win. Resolution order is built-ins → bundled dir (the directory of the symlink-resolved lstk executable) → `PATH`; there is no manifest. Runtime context is conveyed via `LSTK_EXT_API_VERSION` and `LSTK_EXT_CONTEXT` (JSON: `configDir`, optional `authToken`, `nonInteractive`, `json`, `emulators` array) — see `extension.Context`/`Environ` in `internal/extension/context.go`; dispatch and help listing are in `cmd/extension.go`. Automated distribution/co-update of bundled extensions is deferred to the `add-bundled-extension-distribution` change. See [extensions-authoring.md](docs/extensions-authoring.md) for the author-facing contract.

# Snapshots

`lstk snapshot` captures and restores the running emulator's state (for Snowflake and Azure a heads-up is shown that results may be incomplete). Domain logic lives in `internal/snapshot/`; `cmd/snapshot.go` is wiring + output-mode selection. Top-level `lstk save` / `lstk load` are aliases for the save/load subcommands.

- `lstk snapshot save [destination]` / `lstk snapshot load REF` (`--merge`: `account-region-merge` default, `overwrite`, `service-merge`) / `list` (cloud; `--all` for org-wide) / `remove REF` / `show REF` (remove/show are cloud-only).
- A REF is a local `.snapshot` file, a `pod:` cloud snapshot on the LocalStack platform (requires auth), or an `s3://bucket/prefix` remote in the user's own bucket (the emulator performs the transfer; S3 supports save/load/list only).
- A `[[containers]]` block (AWS only) can set `snapshot = "pod:..."` to auto-load after a fresh start; `lstk start --snapshot REF` overrides it for one run, `--no-snapshot` skips it.

REF parsing helpers, S3 credential precedence and remote-upsert mechanics, and the auto-load wiring are documented in `internal/snapshot/CLAUDE.md`.

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

# Shell Completion

Cobra's generated bash completion script requires `_get_comp_words_by_ref` from the bash-completion package on both of its init paths, and stock macOS (bash 3.2) ships without that package — so completion failed with "command not found" on every Tab (DEVX-950). `selfContainBashCompletion` in `cmd/completion.go` wraps the autogenerated `completion bash` command to prepend a guarded pure-bash fallback (defined only when the package is absent, the git-completion.bash approach) and replaces the help text. The fallback body must stay bash 3.2 compatible (no `declare -A`, namerefs, `mapfile`, case-conversion expansions). It covers only `_get_comp_words_by_ref`; Cobra's script still calls bash-completion's `_filedir` for `ShellCompDirectiveFilterFileExt`/`ShellCompDirectiveFilterDirs` (`MarkFlagFilename`/`MarkFlagDirname`) and the ActiveHelp second-Tab path — lstk uses none of these today, so adopting one means growing the fallback. In docs/help, never recommend `source <(lstk completion bash)` — it is a silent no-op on bash 3.2; recommend `eval "$(lstk completion bash)"` instead. Zsh/fish/powershell scripts are self-contained upstream and untouched.

# CLI Help Text

- Write command `Short`/`Long` as unbroken paragraphs (one line each, blank line between); never hard-wrap a sentence in source. `wrapText` in `cmd/help.go` re-wraps to the terminal width at render time and `lstk docs` reads the raw text, so manual breaks fight both. Indented lines (examples, aligned output) are left as-is.

# Writing for Humans

When drafting Slack messages, PR descriptions, review replies, release notes, or README text: keep it short and plain, lead with the point, and produce one tight draft rather than multiple options.

# Testing

- Prefer integration tests to cover most cases. Use unit tests when integration tests are not practical.
- **When fixing a bug, always add an integration test** that fails before the fix and passes after. This prevents regressions and documents the exact scenario that was broken.
- Integration tests that run the CLI binary with Bubble Tea must use a PTY (`github.com/creack/pty`) since Bubble Tea requires a terminal. Use `pty.Start(cmd)` instead of `cmd.CombinedOutput()`, read output with `io.Copy()`, and send keystrokes by writing to the PTY (e.g., `ptmx.Write([]byte("\r"))` for Enter).
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

When making significant changes to the codebase (new commands, architectural changes, build process updates, new patterns), update this CLAUDE.md file to reflect them.

Deep per-feature reference lives next to the code in nested CLAUDE.md files — `internal/config/`, `internal/container/`, `internal/azureconfig/`, `internal/snapshot/` (each with an `AGENTS.md` symlink for non-Claude agents, mirroring the root). Update the nested file when its feature changes; keep this root file for guidance that applies to most sessions.
