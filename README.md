# lstk

**A command-line interface for LocalStack**. Built in Go with a modern terminal UI and native CLI experience for managing and interacting with LocalStack deployments. 👾


```bash
npm install -g @localstack/lstk
```

See [installation](#installation) below.

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

- **Start / stop / status** — manage LocalStack emulators with a single command
- **Interactive TUI** — a Bubble Tea-powered terminal UI shown in an interactive terminal for commands like `start`, `login`, `status`, etc.
- **Plain output** for CI/CD and scripting (auto-detected in non-interactive environments or forced with `--non-interactive`)
- **Log streaming** — tail emulator logs in real-time with `--follow`; use `--verbose` to show all logs without filtering
- **Snapshots** — save, load, and remove emulator state as local files or named cloud snapshots (`pod:` prefix)
- **Browser-based login** — authenticate via browser and store credentials securely in the system keyring
- **AWS CLI profile** — optionally configure a `localstack` profile in `~/.aws/` after start
- **Terraform integration** — proxy Terraform commands to LocalStack with automatic AWS provider endpoint configuration
- **CDK integration** — proxy AWS CDK commands to LocalStack with automatic endpoint configuration (requires AWS CDK >= 2.177.0)
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

`lstk` uses the first `config.toml` found in this order:
1. `./.lstk/config.toml` (project-local)
2. `$HOME/.config/lstk/config.toml`
3. **macOS**: `$HOME/Library/Application Support/lstk/config.toml` / **Windows**: `%AppData%\lstk\config.toml`

On first run, the config is created at `$HOME/.config/lstk/config.toml` if `$HOME/.config/` already exists, otherwise at the OS default (#3). This means #3 is only reached on macOS when `$HOME/.config/` didn't exist at first run.

To see which config file is currently in use:

```bash
lstk config path
```

### Choosing an emulator

`lstk` starts the AWS emulator by default. To run the Snowflake or Azure emulator instead, either select it interactively when prompted at start, or set the `type` in your config:

```toml
[[containers]]
type = "azure"   # or "snowflake"
port = "4566"
```

The chosen emulator must be running before you set up or use its CLI integration below.

You can also configure cloud CLI integration:

```bash
lstk setup aws    # localstack profile in ~/.aws/
lstk setup azure  # isolated Azure CLI config for `lstk az` (requires the Azure CLI)
```

After starting the Azure emulator and running `lstk setup azure`, run Azure CLI commands against LocalStack with `lstk az`:

```bash
lstk az group list
```

`lstk setup azure` registers a custom Azure cloud — pointing at LocalStack's endpoints — inside an isolated `AZURE_CONFIG_DIR`, so your global `~/.azure` keeps pointing at real Azure.

You can also point `lstk` at a specific config file for any command:

```bash
lstk --config /path/to/config.toml start
```

### Default config

```toml
[[containers]]
type = "aws"     # Emulator type. Currently supported: "aws", "snowflake", "azure"
tag  = "latest"  # Docker image tag, e.g. "latest", "2026.03"
port = "4566"    # Host port the emulator will be accessible on
# volume = ""    # Host directory for persistent state (default: OS cache dir)
# env = []       # Named environment profiles to apply (see [env.*] sections below)
```

**Fields:**
- `type`: emulator type; one of `"aws"`, `"snowflake"`, or `"azure"`
- `tag`: Docker image tag for LocalStack (e.g. `"latest"`, `"4.14.0"`); useful for pinning a version
- `port`: port LocalStack listens on (default `4566`)
- `volume`: (optional) host directory for persistent emulator state (default: OS cache dir)
- `env`: (optional) list of named environment variable groups to inject into the container (see below)

### Passing environment variables to the container

Define reusable named env sets and reference them per container:

```toml
[[containers]]
type = "aws"
tag  = "latest"
port = "4566"
env  = ["debug", "ci"]

[env.debug]
DEBUG = "1"
ENFORCE_IAM = "1"
PERSISTENCE = "1"

[env.ci]
SERVICES = "s3,sqs"
EAGER_SERVICE_LOADING = "1"
```

Host environment variables prefixed with `LOCALSTACK_` are also forwarded to the emulator.

## Interactive And Non-Interactive Mode

`lstk` uses the TUI in an interactive terminal and plain output elsewhere. Use `--non-interactive` to force plain output even in a TTY:

```bash
lstk --non-interactive
```

## Logging

`lstk` writes diagnostic logs to `lstk.log` in the same directory as the config file. The log file appends across runs and is automatically cleared when it exceeds 1 MB. Use `lstk config path` to print the full config file path; the log file lives alongside it in the same directory.

## Environment Variables

| Variable | Description |
|---|---|
| `LOCALSTACK_AUTH_TOKEN` | Auth token used for non-interactive runs or to skip browser login |
| `LOCALSTACK_DISABLE_EVENTS=1` | Disables telemetry event reporting |
| `LSTK_OTEL=1` | Enables OpenTelemetry trace export (disabled by default). When enabled, standard `OTEL_EXPORTER_OTLP_*` env vars are respected by the SDK (e.g. `OTEL_EXPORTER_OTLP_ENDPOINT` defaults to `http://localhost:4318`). Requires an OTLP-compatible backend to receive and visualize telemetry — for local development, `make otel` starts one (UI at http://localhost:16686). |
| `DOCKER_HOST` | Override the Docker daemon socket (e.g. `unix:///home/user/.colima/default/docker.sock`). When unset, lstk tries the default socket and then probes common alternatives (Colima, OrbStack). |

### Terraform Integration

`lstk terraform` (alias `tf`) is a proxy that runs Terraform commands against LocalStack, automatically configuring the AWS provider to use LocalStack's endpoints. This allows you to test infrastructure-as-code locally before deploying to AWS.

**lstk-specific flags** (appear after the `terraform`/`tf` subcommand):
- `--region <region>` — Deployment region (default: `us-east-1`)
- `--account <id>` — Target AWS account ID, 12 digits (default: `test`)

**Environment variables:**
- `LSTK_TF_CMD` — Terraform binary to invoke; default is `terraform` (e.g., use `tofu` for OpenTofu)
- `LSTK_TF_OVERRIDE_FILE_NAME` — Override file name (default: `localstack_providers_override.tf`)
- `LSTK_TF_DRY_RUN` — Generate the override file but do not run terraform
- `AWS_ENDPOINT_URL` — Override the auto-resolved LocalStack endpoint
- `AWS_REGION` — Fallback for `--region` flag
- `AWS_ACCESS_KEY_ID` — Fallback for `--account` flag

### CDK Integration

`lstk cdk` is a proxy that runs AWS CDK commands against LocalStack, pointing the CDK CLI at LocalStack's endpoints via environment variables (so deploys target the running emulator instead of real AWS).

**Requires AWS CDK CLI version 2.177.0 or newer** on your `PATH` (lstk targets LocalStack purely through environment variables, which older CDK versions ignore).

**lstk-specific flags** (appear after the `cdk` subcommand):
- `--region <region>` — Deployment region (default: `us-east-1`)

CDK always targets the default LocalStack account 000000000000; there is no --account flag.

**Environment variables:**
- `LSTK_CDK_CMD` — CDK binary to invoke (default: `cdk`)
- `AWS_ENDPOINT_URL` — Override the auto-resolved LocalStack endpoint
- `AWS_ENDPOINT_URL_S3` — Override the auto-derived S3 endpoint
- `AWS_REGION` — Fallback for `--region` flag

## Usage

```bash
# Start the LocalStack emulator
lstk

# Start non-interactively (e.g. in CI)
LOCALSTACK_AUTH_TOKEN=<token> lstk --non-interactive

# Stop the running emulator
lstk stop

# Show emulator status and deployed resources
lstk status

# Stream emulator logs
lstk logs --follow

# Stream all emulator logs without filtering
lstk logs --follow --verbose

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

# Set up AWS CLI profile integration
lstk setup aws

# Set up Azure CLI integration (isolated config for `lstk az`)
lstk setup azure

# Run Azure CLI commands against LocalStack
lstk az group list

# Save emulator state to a local file
lstk snapshot save ./my-snapshot.snapshot

# Save emulator state as a named cloud snapshot on the LocalStack platform
lstk snapshot save pod:my-baseline

# Load a snapshot back into the running emulator
lstk snapshot load pod:my-baseline

# List cloud snapshots on the LocalStack platform (--all for the whole organization)
lstk snapshot list

# Delete a cloud snapshot (prompts for confirmation; --force to skip)
lstk snapshot remove pod:my-baseline

# Initialize Terraform with LocalStack
lstk terraform init

# Plan Terraform deployment in a specific region
lstk terraform --region us-west-2 plan

# Apply Terraform configuration (short form)
lstk tf apply

# Bootstrap and deploy an AWS CDK app against LocalStack
lstk cdk bootstrap
lstk cdk deploy --require-approval never

# Synthesize a CDK app (offline, no running emulator needed)
lstk cdk synth

```

## Snapshots

Snapshots capture the running emulator's state so you can restore it later.

A snapshot reference is either a **local file** or a **cloud snapshot**:

- **Local file** — an absolute or relative path. A `.snapshot` extension is added if omitted (snapshots saved as `.zip` by older lstk versions still load).
- **Cloud snapshot** — a name with the `pod:` prefix (e.g. `pod:my-baseline`), stored on the LocalStack platform. Requires authentication (`LOCALSTACK_AUTH_TOKEN` or `lstk login`).

```bash
# Save (local or cloud)
lstk snapshot save ./my-snapshot.snapshot
lstk snapshot save pod:my-baseline

# Load (starts the emulator first if needed)
lstk snapshot load pod:my-baseline

# List cloud snapshots — only your own by default, --all for the whole organization
lstk snapshot list
lstk snapshot list --all

# Remove — cloud snapshots only; local files are never deleted by the CLI
lstk snapshot remove pod:my-baseline          # prompts for confirmation
lstk snapshot remove pod:my-baseline --force  # skip the prompt (required in non-interactive mode)
```

`lstk snapshot load` supports merge strategies via `--merge` (`account-region-merge` (default), `overwrite`, `service-merge`) to control how snapshot state combines with running state.

## Reporting bugs

Feedback is welcome! Use the repository issue tracker for bug reports or feature requests.
