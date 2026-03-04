# lstk

**lstk** is LocalStack's CLI v2 — a fast, interactive command-line interface for starting and managing LocalStack emulators via Docker.

## Installation

### Homebrew (macOS / Linux)

```bash
brew install localstack/tap/lstk
```

### npm / npx

Install globally:

```bash
npm install -g @localstack/lstk
lstk start
```

Or run without installing:

```bash
npx @localstack/lstk start
```

### Manual (binary download)

Download the latest release for your platform from the [GitHub Releases](https://github.com/localstack/lstk/releases) page. Binaries are available for:

- `linux/amd64`, `linux/arm64`
- `darwin/amd64`, `darwin/arm64` (macOS)
- `windows/amd64`, `windows/arm64`

## Requirements

- [Docker](https://docs.docker.com/get-docker/) must be running
- A [LocalStack Auth Token](https://app.localstack.cloud) (for Pro features) — or set `LOCALSTACK_AUTH_TOKEN` in your environment

## Features

- **Start / stop** LocalStack emulators with a single command
- **Interactive TUI** — a Bubble Tea-powered terminal UI when run in an interactive shell
- **Plain output** for CI/CD and scripting (auto-detected in non-interactive environments)
- **Log streaming** — tail emulator logs in real-time with `--follow`
- **Browser-based login** — authenticate via browser and store credentials securely in the system keyring
- **Shell completions** — bash, zsh, and fish completions included

## Usage

```bash
# Start the LocalStack emulator (interactive TUI in a terminal)
lstk start

# Start non-interactively (e.g. in CI)
LOCALSTACK_AUTH_TOKEN=<token> lstk start

# Stop the running emulator
lstk stop

# Stream emulator logs
lstk logs --follow

# Log in (opens browser for authentication)
lstk login

# Log out (removes stored credentials)
lstk logout

# Show resolved config file path
lstk config path

# Show version info
lstk version
```

## Authentication

You can authenticate in two ways:

1. **Environment variable** — set `LOCALSTACK_AUTH_TOKEN` before running any command:
   ```bash
   export LOCALSTACK_AUTH_TOKEN=<your-token>
   ```
2. **Browser login** — run `lstk login` and follow the browser prompt. Credentials are stored in your system keyring.

## Configuration

`lstk` uses a TOML config file, created automatically on first run.

Config lookup order:
1. `./lstk.toml` (project-local)
2. `$HOME/.config/lstk/config.toml`
3. `os.UserConfigDir()/lstk/config.toml`

To see which config file is currently in use:

```bash
lstk config path
```

## Versioning

`lstk` uses [ZeroVer](https://0ver.org/) (`0.MINOR.PATCH`). The project is in active development and has not reached a stable 1.0 release.

## Releasing with GoReleaser

Release automation uses two workflows:

1. `Create Release Tag` (`.github/workflows/create-release-tag.yml`)
2. `LSTK CI` (`.github/workflows/ci.yml`)

How it works:

1. Manually run `Create Release Tag` from GitHub Actions (default ref: `main`), choosing a `patch` or `minor` bump.
2. The workflow computes and pushes the next version tag (e.g. `v0.2.4`).
3. Pushing the tag triggers `LSTK CI`, which runs the `release` job and publishes the GitHub release with GoReleaser.

To validate release packaging locally without publishing:

```bash
goreleaser release --snapshot --clean
```
