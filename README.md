# lstk

lstk is a command-line interface for LocalStack built in Go with a modern terminal Ul, and native CLI experience for starting and managing LocalStack deployments. 👾

## Features

- **Start / stop** LocalStack emulators with a single command
- **Interactive TUI** — a Bubble Tea-powered terminal UI when run in an interactive shell
- **Plain output** for CI/CD and scripting (auto-detected in non-interactive environments)
- **Log streaming** — tail emulator logs in real-time with `--follow`
- **Browser-based login** — authenticate via browser and store credentials securely in the system keyring
- **Shell completions** — bash, zsh, and fish completions included

## Installation

### Homebrew (macOS / Linux)

```bash
brew install localstack/tap/lstk
```

### NPM

```bash
npm install -g @localstack/lstk
lstk start
```

### Manual (binary download)

Download the latest release for your platform from the [GitHub Releases](https://github.com/localstack/lstk/releases) page. Binaries are available for:

- `linux/amd64`, `linux/arm64`
- `darwin/amd64`, `darwin/arm64` (macOS)
- `windows/amd64`, `windows/arm64`

## Requirements

- [Docker](https://docs.docker.com/get-docker/) must be running
- A [LocalStack Account](https://app.localstack.cloud) 


## Usage

```bash
# Start the LocalStack emulator (interactive TUI in a terminal)
lstk

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

`lstk` resolves your auth token in this order:

1. **System keyring** — a token stored by a previous `lstk login`
2. **`LOCALSTACK_AUTH_TOKEN` environment variable**
3. **Browser login** — triggered automatically in interactive mode when neither of the above is present

> **Note:** If a keyring token exists, it takes precedence over `LOCALSTACK_AUTH_TOKEN`. Setting or changing the environment variable will have no effect until the keyring token is removed. Run `lstk logout` to clear the stored keyring token, after which the env var will be used.

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


## Reporting bugs
Feedback is welcome! Use the repository issue tracker for bug reports or feature requests.

