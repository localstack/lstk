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

# Maintaining This File

When making significant changes to the codebase (new commands, architectural changes, build process updates, new patterns), update this CLAUDE.md file to reflect them.
