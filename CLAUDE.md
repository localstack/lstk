# Project Overview

lstk is LocalStack's new CLI (v2) - a Go-based command-line interface for starting and managing LocalStack instances via Docker (and more runtimes in the future).

# Build and Test Commands

```bash
make build              # Compiles to bin/lstk
make test-integration   # Run integration tests (builds first, requires Docker)
make clean              # Remove build artifacts
```

Run a single integration test:
```bash
cd test/integration && go test -count=1 -v -run TestStartCommandSucceedsWithValidToken .
```

Note: Integration tests require `LOCALSTACK_AUTH_TOKEN` environment variable for valid token tests.

# Architecture

- `main.go` - Entry point
- `cmd/` - CLI wiring only (Cobra framework), no business logic
- `internal/` - All business logic goes here
  - `container/` - Handling different emulator containers
  - `runtime/` - Abstraction for container runtimes (Docker, Kubernetes, etc.) - currently only Docker implemented
  - `auth/` - Authentication (env var token or browser-based login)
  - `output/` - Generic event and sink abstractions for CLI/TUI/non-interactive rendering
  - `ui/` - Bubble Tea views for interactive output

# Configuration

Uses Viper with TOML format. Config file location:
- Linux: `~/.config/lstk/config.toml`
- macOS: `~/Library/Application Support/lstk/config.toml`

Created automatically on first run with defaults. Supports emulator types (aws, snowflake, azure) - currently only aws is implemented.

Environment variables:
- `LOCALSTACK_AUTH_TOKEN` - Auth token (skips browser login if set)

# Code Style

- Don't add comments for self-explanatory code. Only comment when the "why" isn't obvious from the code itself.
- Do not remove comments added by someone else than yourself.
- Errors returned by functions should always be checked unless in test files.

# Testing

- Prefer integration tests to cover most cases. Use unit tests when integration tests are not practical.

# Output Routing and Events

- Emit typed events through `internal/output` (`EmitLog`, `EmitStatus`, `EmitProgress`, etc.) instead of printing from domain/command handlers.
- Keep `output.Sink` sealed (unexported `emit`); sink implementations belong in `internal/output`.
- Reuse `FormatEventLine(event any)` for all line-oriented rendering so plain and TUI output stay consistent.
- Select output mode at the command boundary in `cmd/`: interactive TTY runs Bubble Tea, non-interactive mode uses `output.NewPlainSink(...)`.
- Keep non-TTY mode non-interactive (no stdin prompts or input waits).
- Domain packages must not import Bubble Tea or UI packages.
- Any feature/workflow package that produces user-visible progress should accept an `output.Sink` dependency and emit events through `internal/output`.
- Do not pass UI callbacks like `onProgress func(...)` through domain layers; prefer typed output events.
- Event payloads should be domain facts (phase/status/progress), not pre-rendered UI strings.
- When adding a new event type, update all of:
  - `internal/output/events.go` (event type + union + emit helper)
  - `internal/output/format.go` (line formatting fallback)
  - tests in `internal/output/*_test.go` for formatter/sink behavior parity

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

# Maintaining This File

When making significant changes to the codebase (new commands, architectural changes, build process updates, new patterns), update this CLAUDE.md file to reflect them.
