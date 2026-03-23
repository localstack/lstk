# Contributing to lstk

Thanks for contributing to lstk! This document covers contribution guidelines for the lstk CLI.

## Development Setup

### Prerequisites

- Go 1.21+ (or latest stable)
- Docker (for integration tests)
- Make

### Building

```bash
make build              # Compiles to bin/lstk
```

### Running Tests

```bash
make test               # Run unit tests (cmd/ and internal/) via gotestsum
make test-integration   # Run integration tests (requires Docker)
make lint               # Run golangci-lint
```

To run a single integration test:

```bash
make test-integration RUN=TestStartCommandSucceedsWithValidToken
```

## Code Style

- Comments: only for non-obvious "why" decisions
- Error handling: always check returned errors (except in tests)
- No package-level global variables; use constructors and dependency injection
- No direct stdout/stderr printing; use `output.Sink` for user output, `log.Logger` for diagnostics

## Architecture

- `cmd/` — CLI wiring only (Cobra), no business logic
- `internal/` — all business logic:
  - `container/` — emulator container handling
  - `runtime/` — container runtime abstraction (Docker)
  - `auth/` — authentication (env token, keyring, browser login)
  - `config/` — Viper-based TOML config
  - `output/` — event/sink system for CLI/TUI rendering
  - `ui/` — Bubble Tea views
  - `update/` — self-update logic
  - `log/` — internal diagnostic logging

See `CLAUDE.md` for full architecture details.

## Adding Features

### New Commands

Use the skill:
```
/add-command <name>
```

This scaffolds a new CLI subcommand with proper wiring, domain logic, and tests.

### New Output Events

Use the skill:
```
/add-event <EventName>
```

This adds a new typed event to the `output/` event system.

### New UI Components

Use the skill:
```
/add-component <name>
```

This scaffolds a new Bubble Tea TUI component.

## Testing Guidelines

- Prefer integration tests for most behavior
- Unit tests for isolated logic where integration is impractical
- When fixing a bug, always add an integration test that reproduces the scenario

## Pull Requests

1. Fork the repository
2. Create a feature branch from `main`
3. Run `make lint` and `make test` before submitting
4. Review your own PR first — run `/review-pr <number>`
5. Open a PR against `main`

## Questions?

Check `README.md` for usage docs and `CLAUDE.md` for architecture.
