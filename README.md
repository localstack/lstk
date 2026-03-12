# lstk

**A command-line interface for LocalStack**. Built in Go with a modern terminal UI and native CLI experience for managing and interacting with LocalStack deployments. 👾


```bash
npm install -g @localstack/lstk
```

See [installation](#installation) below.

> [!IMPORTANT]
> This project is under active development, currently using [ZeroVer](https://0ver.org/) (`0.MINOR.PATCH`). Expect breaking changes as we march toward a stable 1.0.0 release.


## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) — required as a container engine.
- [LocalStack account](https://app.localstack.cloud) — required for credentials, the CLI will guide you through authentication.

## Installation

### 1. Homebrew (macOS / Linux)

```bash
brew install localstack/tap/lstk
```

### 2. NPM

```bash
npm install -g @localstack/lstk
```

### 3. Binaries
Pre-built binaries are also available from [GitHub Releases](https://github.com/localstack/lstk/releases). 📦

## Quick Start

```sh
lstk
```

Running `lstk` will automatically handle configuration setup and start LocalStack.

## Features

- **Start / stop** LocalStack emulators with a single command
- **Interactive TUI** — a Bubble Tea-powered terminal UI when run in an interactive shell
- **Plain output** for CI/CD and scripting (auto-detected in non-interactive environments or forced with `--non-interactive`)
- **Log streaming** — tail emulator logs in real-time with `--follow`
- **Browser-based login** — authenticate via browser and store credentials securely in the system keyring
- **AWS CLI profile** — optionally configure a `localstack` profile in `~/.aws/` after start
- **Self-update** — check for and install the latest `lstk` release with `lstk update`
- **Shell completions** — bash, zsh, and fish completions included

## Authentication

The CLI supports multiple auth workflows. `lstk` resolves your auth token in this order:

1. **System keyring** — a token stored by a previous `lstk login`
2. **`LOCALSTACK_AUTH_TOKEN` environment variable**
3. **Browser login** — triggered automatically in interactive mode when neither of the above is present

> [!NOTE]
> If a keyring token exists, it takes precedence over `LOCALSTACK_AUTH_TOKEN`. Setting or changing the environment variable will have no effect until the keyring token is removed. Run `lstk logout` to clear the stored keyring token, after which the env var will be used.


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

You can also point `lstk` at a specific config file for any command:

```bash
lstk --config /path/to/lstk.toml start
```

### Default config

```toml
[[containers]]
type = "aws"
tag  = "latest"
port = "4566"
```

**Fields:**
- `type`: emulator type; only `"aws"` is supported for now
- `tag`: Docker image tag for LocalStack (e.g. `"latest"`, `"4.14.0"`); useful for pinning a version
- `port`: port LocalStack listens on (default `4566`)
- `env`: (optional) list of named environment variable groups to inject into the container (see below)

### Passing environment variables to the container

Define reusable named env sets and reference them per container:

```toml
[[containers]]
type = "aws"
tag  = "4.14.0"
port = "4566"
env  = ["prod", "debug"]

[env.prod]
LOCALSTACK_HOST = "localstack.cloud"

[env.debug]
LS_LOG = "trace"
DEBUG  = "1"
```

## Interactive And Non-Interactive Mode

`lstk` uses the TUI in an interactive terminal and plain output elsewhere. Use `--non-interactive` to force plain output even in a TTY:

```bash
lstk --non-interactive
```

## Environment Variables

| Variable | Description |
|---|---|
| `LOCALSTACK_AUTH_TOKEN` | Auth token used for non-interactive runs or to skip browser login |
| `LOCALSTACK_DISABLE_EVENTS=1` | Disables telemetry event reporting |

## Usage

```bash
# Start the LocalStack emulator (interactive TUI in a terminal)
lstk

# Start non-interactively (e.g. in CI)
LOCALSTACK_AUTH_TOKEN=<token> lstk --non-interactive

# Stop the running emulator
lstk stop

# Stream emulator logs
lstk logs --follow

# Log in (opens browser for authentication)
lstk login

# Log out (removes stored credentials)
lstk logout

# Check whether a newer lstk version is available
lstk update --check

# Update lstk to the latest version
lstk update

# Show resolved config file path
lstk config path

# Show version info
lstk version
```

## Reporting bugs

Feedback is welcome! Use the repository issue tracker for bug reports or feature requests.
