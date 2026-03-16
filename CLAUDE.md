# Project Overview

lstk is LocalStack's new CLI (v2) - a Go-based command-line interface for starting and managing LocalStack instances via Docker (and more runtimes in the future).

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

# Configuration

Uses Viper with TOML format. lstk uses the first config file found in this order:
1. `./lstk.toml` (project-local)
2. `$HOME/.config/lstk/config.toml`
3. **macOS**: `$HOME/Library/Application Support/lstk/config.toml` / **Windows**: `%AppData%\lstk\config.toml`

When no config file exists, lstk creates one at `$HOME/.config/lstk/config.toml` if `$HOME/.config/` already exists, otherwise at the OS default (#3). This means #3 is only reached on macOS when `$HOME/.config/` didn't exist at first run.

Use `lstk config path` to print the resolved config file path currently in use.
When adding a new command that depends on configuration, wire config initialization explicitly in that command (`PreRunE: initConfig`). Keep side-effect-free commands (e.g., `version`, `config path`) without config initialization.

Created automatically on first run with defaults. Supports emulator types (aws, snowflake, azure) - currently only aws is implemented.

Environment variables:
- `LOCALSTACK_AUTH_TOKEN` - Auth token (skips browser login if set)

# Code Style

- Don't add comments for self-explanatory code. Only comment when the "why" isn't obvious from the code itself.
- Do not remove comments added by someone else than yourself.
- Errors returned by functions should always be checked unless in test files.
- Terminology: in user-facing CLI/help/docs, prefer `emulator` over `container`/`runtime`; use `container`/`runtime` only for internal implementation details.
- Avoid package-level global variables. Use constructor functions that return fresh instances and inject dependencies explicitly. This keeps packages testable in isolation and prevents shared mutable state between tests.
- Do not call `config.Get()` from domain/business-logic packages. Instead, extract the values you need at the command boundary (`cmd/`) and pass them as explicit function arguments. This keeps domain functions testable without requiring Viper/config initialization.

# Testing

- Prefer integration tests to cover most cases. Use unit tests when integration tests are not practical.
- Integration tests that run the CLI binary with Bubble Tea must use a PTY (`github.com/creack/pty`) since Bubble Tea requires a terminal. Use `pty.Start(cmd)` instead of `cmd.CombinedOutput()`, read output with `io.Copy()`, and send keystrokes by writing to the PTY (e.g., `ptmx.Write([]byte("\r"))` for Enter).

# Output Routing and Events

- Emit typed events through `internal/output` (`EmitInfo`, `EmitSuccess`, `EmitNote`, `EmitWarning`, `EmitStatus`, `EmitProgress`, etc.) instead of printing from domain/command handlers.
- Keep `output.Sink` sealed (unexported `emit`); sink implementations belong in `internal/output`.
- Reuse `FormatEventLine(event any)` for all line-oriented rendering so plain and TUI output stay consistent.
- Select output mode at the command boundary in `cmd/`: interactive TTY runs Bubble Tea, non-interactive mode uses `output.NewPlainSink(...)`.
- Keep non-TTY mode non-interactive (no stdin prompts or input waits).
- Domain packages must not import Bubble Tea or UI packages.
- Any feature/workflow package that produces user-visible progress should accept an `output.Sink` dependency and emit events through `internal/output`.
- Do not pass UI callbacks like `onProgress func(...)` through domain layers; prefer typed output events.
- Event payloads should be domain facts (phase/status/progress), not pre-rendered UI strings.
- When adding a new event type, update all of:
  - `internal/output/events.go` (event type + `Event` union constraint + emit helper)
  - `internal/output/plain_format.go` (line formatting fallback)
  - tests in `internal/output/*_test.go` for formatter/sink behavior parity

## User Input Handling

Domain code must never read from stdin or wait for user input directly. Instead:

1. Emit a `UserInputRequestEvent` via `output.EmitUserInputRequest()` with:
   - `Prompt`: message to display
   - `Options`: available choices (e.g., `{Key: "enter", Label: "Press ENTER to continue"}`)
   - `ResponseCh`: channel to receive the user's response

2. Wait on the `ResponseCh` for an `InputResponse` containing:
   - `SelectedKey`: which option was selected
   - `Cancelled`: true if user cancelled (e.g., Ctrl+C)

3. The TUI (`internal/ui/app.go`) handles these events by showing the prompt and sending the response when the user interacts.

4. In non-interactive mode, commands requiring user input should fail early with a helpful error (e.g., "set LOCALSTACK_AUTH_TOKEN or run in interactive mode").

Example flow in auth login:
```go
responseCh := make(chan output.InputResponse, 1)
output.EmitUserInputRequest(sink, output.UserInputRequestEvent{
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
