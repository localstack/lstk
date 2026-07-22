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
- **Snapshots** — save, load, and remove emulator state as local files, named cloud snapshots (`pod:` prefix), or in your own S3 bucket (`s3://`), and auto-load one on start
- **Browser-based login** — authenticate via browser and store credentials securely in the system keyring
- **AWS CLI proxy** — run `lstk aws <args>` with endpoint, credentials, and region pre-configured
- **AWS CLI profile** — optionally configure a `localstack` profile in `~/.aws/` after start
- **Terraform integration** — proxy Terraform commands to LocalStack with automatic AWS provider endpoint configuration
- **CDK integration** — proxy AWS CDK commands to LocalStack with automatic endpoint configuration (requires AWS CDK >= 2.177.0)
- **SAM integration** — proxy AWS SAM CLI commands to LocalStack (requires AWS SAM CLI >= 1.95.0)
- **Reset / restart** — clear in-memory emulator state with `lstk reset`, or restart the emulator with `lstk restart`
- **Extensions** — Git-style `lstk-<name>` executables extend the CLI with new commands
- **Self-update** — check for and install the latest `lstk` release with `lstk update`
- **Shell completions** — bash, zsh, and fish completions included
- **Structured JSON output** — pass `--json` to a supported command (`stop`, `reset`, `update` today, more planned) for a machine-readable envelope instead of formatted text; see [docs/structured-output.md](docs/structured-output.md)

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

For CI or other non-interactive use, pass `--type` (shorthand `-t`) to select the emulator without editing config. It records the selection in your config file — creating it on first run, or switching the `type` in place when it differs — so later commands (`stop`, `status`, `logs`, `volume`, …) resolve the same emulator:

```bash
LOCALSTACK_AUTH_TOKEN=<token> lstk start --type azure --non-interactive
```

Switching an existing config keeps the other block fields; a custom `image` blocks the switch (it pins a specific product — use a separate `--config` file instead).

The chosen emulator must be running before you set up or use its CLI integration below.

> [!NOTE]
> Only one `[[containers]]` block can be enabled at a time — running multiple emulators together (e.g. AWS and Snowflake) isn't supported yet.

You can also configure cloud CLI integration:

```bash
lstk setup aws    # localstack profile in ~/.aws/
lstk setup azure  # isolated Azure CLI config for `lstk az` (requires the Azure CLI); alias: lstk setup az
```

After starting the Azure emulator and running `lstk setup azure`, run Azure CLI commands against LocalStack with `lstk az`:

```bash
lstk az group list
```

`lstk setup azure` registers a custom Azure cloud — pointing at LocalStack's endpoints — inside an isolated `AZURE_CONFIG_DIR`, so your global `~/.azure` keeps pointing at real Azure.

To run existing `az` scripts unmodified against LocalStack, you can instead redirect your **global** Azure CLI:

```bash
lstk az start-interception   # plain `az` now targets LocalStack
az group list                # hits LocalStack, no `lstk` prefix needed
lstk az stop-interception    # back to real Azure (use --cloud to pick another cloud)
```

This is optional and changes global state affecting every `az` invocation until you stop it; prefer `lstk az <command>` unless a script must call plain `az`.

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
# image = ""     # Override the default Docker image, e.g. an internal registry mirror (see below)
# volumes = []   # Bind mounts, "host:container[:ro]" (see below)
# env = []       # Named environment profiles to apply (see [env.*] sections below)
# snapshot = "pod:my-baseline"  # Snapshot REF auto-loaded on start (AWS only); see Snapshots below
```

**Fields:**
- `type`: emulator type; one of `"aws"`, `"snowflake"`, or `"azure"`
- `tag`: Docker image tag for LocalStack (e.g. `"latest"`, `"4.14.0"`); useful for pinning a version
- `port`: port LocalStack listens on (default `4566`)
- `image`: (optional) override the default `localstack/<product>:<tag>` image, e.g. `"my-registry.example.com/localstack:latest"` for an internal mirror or a locally loaded offline image; if it already carries a tag, `tag` above is ignored
- `volumes`: (optional) list of `"host:container[:ro]"` bind mounts, e.g. for init hooks or the persistent-state directory (see below)
- `env`: (optional) list of named environment variable groups to inject into the container (see below)
- `snapshot`: (optional) snapshot REF auto-loaded after the emulator starts on a fresh run — a local file path or a `pod:` cloud snapshot (see [Snapshots](#snapshots))

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

### Exposing the emulator beyond localhost

By default the gateway (and its published ports) are only reachable from `localhost`. To expose it more broadly (e.g. on an EC2 or VM host), set `GATEWAY_LISTEN` in an `[env.*]` profile:

```toml
[[containers]]
type = "aws"
port = "4566"
env  = ["public"]

[env.public]
GATEWAY_LISTEN = "0.0.0.0:4566,0.0.0.0:443"
```

The host part of the first entry becomes the bind address for every published port (the gateway ports and the 4510-4559 service range); it defaults to `127.0.0.1` when unset.

### Mounting volumes and init hooks

Use `volumes` to bind-mount host files or directories into the emulator, given as Docker-style `"host:container[:ro]"` strings. The most common use is [init hooks](https://docs.localstack.cloud/snowflake/capabilities/init-hooks/) — scripts LocalStack runs automatically on startup when mounted into `/etc/localstack/init/{boot,start,ready,shutdown}.d`:

```toml
[[containers]]
type = "snowflake"
port = "4566"
volumes = ["./init.sf.sql:/etc/localstack/init/ready.d/init.sf.sql"]
```

- Relative host paths resolve against the config file's directory, and a leading `~/` is expanded.
- Append `:ro` to mount read-only.
- Host sources must already exist (init-hook entries are files, so `lstk` does not create them).

#### Persistent state

The persistent-state directory (mounted at `/var/lib/localstack`, managed by `lstk volume path` / `lstk volume clear`) defaults to the OS cache dir. Point it elsewhere with a `volumes` entry targeting that path:

```toml
volumes = ["/data:/var/lib/localstack"]
```

> The singular `volume = "..."` field is a legacy way to set only this directory. It still works, but `volumes` is preferred and is the only option for init hooks or other mounts.

### Offline / enterprise environments

`lstk start` degrades gracefully when the common enterprise blockers (Docker Hub unreachable, a forward proxy, TLS interception, or an unreachable license server) make a network request fail:

- If the image cannot be pulled but is already present locally, `lstk` warns and starts the local image instead of failing.
- If the license server cannot be reached, or it rejects the configured image tag as unrecognized (e.g. a `dev` nightly or a custom enterprise-mirror tag), `lstk` skips its pre-flight check and lets the emulator validate the license at startup. A definitive rejection from the server (e.g. an invalid token) stays fatal.

Pair this with a custom `image` in the config to point at a locally loaded image or an internal-registry mirror.

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
| `LSTK_STARTUP_TIMEOUT` | How long `lstk start` waits for the emulator to become healthy before acting (Go duration, e.g. `90s`, `5m`). Defaults: 20s interactively (shows a keep-waiting/stop prompt; "keep waiting" re-arms the deadline), 60s non-interactively (fails with the last container logs, leaving the emulator running for inspection). |
| `LSTK_OTEL=1` | Enables OpenTelemetry trace export (disabled by default). When enabled, standard `OTEL_EXPORTER_OTLP_*` env vars are respected by the SDK (e.g. `OTEL_EXPORTER_OTLP_ENDPOINT` defaults to `http://localhost:4318`). Requires an OTLP-compatible backend to receive and visualize telemetry — for local development, `make otel` starts one (UI at http://localhost:16686). |
| `DOCKER_HOST` | Override the Docker daemon socket (e.g. `unix:///home/user/.colima/default/docker.sock`). When unset, lstk tries the default socket and then probes common alternatives (Colima, OrbStack). |

### AWS CLI Proxy

`lstk aws <args>` runs the AWS CLI against LocalStack with the endpoint, credentials, and region pre-configured — equivalent to `aws --endpoint-url http://localhost:4566 <args>` with `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_DEFAULT_REGION` set automatically. Requires the AWS CLI on your `PATH`. This is separate from `lstk setup aws`, which configures a persistent `localstack` profile in `~/.aws/` instead.

```bash
lstk aws s3 ls
lstk aws sqs list-queues
```

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

### SAM Integration

`lstk sam` is a proxy that runs AWS SAM CLI commands against LocalStack, automatically configuring the endpoint and credentials.

**Requires AWS SAM CLI version 1.95.0 or newer** on your `PATH` (older versions ignore `AWS_ENDPOINT_URL` and would target real AWS).

**lstk-specific flags** (appear after the `sam` subcommand):
- `--region <region>` — Deployment region (default: `us-east-1`)
- `--account <id>` — Target AWS account ID, 12 digits (default: `000000000000`)

**Environment variables:**
- `LSTK_SAM_CMD` — SAM binary to invoke (default: `sam`)
- `AWS_ENDPOINT_URL` — Override the auto-resolved LocalStack endpoint
- `AWS_ENDPOINT_URL_S3` — Override the auto-derived S3 endpoint
- `AWS_REGION` — Fallback for `--region` flag
- `AWS_ACCESS_KEY_ID` — Fallback for `--account` flag

Known limitations versus `samlocal`: image/container-based Lambda (ECR) deploys and nested CloudFormation stacks are not supported.

### eksctl Integration

`lstk eksctl` is a proxy that runs [eksctl](https://eksctl.io/) commands against LocalStack, pointing eksctl at LocalStack's endpoints via environment variables so cluster operations target the running emulator instead of real AWS. This replaces the manual export of several `AWS_*_ENDPOINT` variables documented for the ["Newer Versions" flow](https://docs.localstack.cloud/aws/customization/kubernetes/eksctl/) in the LocalStack docs.

**Requires eksctl version 0.181.0 or newer** on your `PATH` — the boundary the LocalStack docs define for the environment-variable flow; lstk rejects older versions rather than run a flow it doesn't support. EKS is included in LocalStack's Ultimate plan (the community image does not support it), and you'll need `kubectl` to interact with the created cluster.

lstk sets the CloudFormation, EC2, EKS, ELB, ELBv2, IAM, and STS service endpoints (plus the generic `AWS_ENDPOINT_URL`) to the resolved LocalStack endpoint. It clears inherited service-specific endpoint overrides and AWS profile/session configuration so they cannot take precedence over the LocalStack endpoint.

**Environment variables:**
- `LSTK_EKSCTL_CMD` — eksctl binary to invoke (default: `eksctl`)
- `AWS_ENDPOINT_URL` — Overrides the auto-resolved LocalStack endpoint
- `AWS_REGION` — Deployment region (default: `us-east-1`)
- `AWS_ACCESS_KEY_ID` — Access key LocalStack derives the account from (default: `test`)

```bash
lstk eksctl create cluster --nodes 1
lstk eksctl get clusters
```

eksctl support in LocalStack is experimental and may not work in all cases.

## Usage

```bash
# Start the LocalStack emulator
lstk

# Start non-interactively (e.g. in CI)
LOCALSTACK_AUTH_TOKEN=<token> lstk --non-interactive

# Select the emulator non-interactively (records it in config)
LOCALSTACK_AUTH_TOKEN=<token> lstk start --type snowflake --non-interactive

# Stop the running emulator
lstk stop

# Restart the emulator
lstk restart

# Clear in-memory emulator state (buckets, functions, etc.) without stopping it
lstk reset

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

# Run AWS CLI commands against LocalStack
lstk aws s3 ls

# Set up AWS CLI profile integration
lstk setup aws

# Set up Azure CLI integration (isolated config for `lstk az`)
lstk setup azure

# Run Azure CLI commands against LocalStack
lstk az group list

# Or redirect your global `az` so existing scripts hit LocalStack unmodified
lstk az start-interception
lstk az stop-interception

# Save emulator state to a local file (`lstk save` is a shortcut for `lstk snapshot save`)
lstk snapshot save ./my-snapshot.snapshot

# Save emulator state as a named cloud snapshot on the LocalStack platform
lstk snapshot save pod:my-baseline

# Save to your own S3 bucket (credentials from AWS_* env vars or --profile)
lstk snapshot save my-pod s3://my-bucket/prefix

# Load a snapshot back into the running emulator (`lstk load` is a shortcut for `lstk snapshot load`)
lstk snapshot load pod:my-baseline
lstk snapshot load my-pod s3://my-bucket/prefix

# List cloud snapshots on the LocalStack platform (--all for the whole organization)
lstk snapshot list

# List snapshots in your own S3 bucket (requires a running emulator)
lstk snapshot list s3://my-bucket/prefix

# Show metadata for a single cloud snapshot
lstk snapshot show pod:my-baseline

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

# Build and deploy a SAM app against LocalStack
lstk sam build
lstk sam deploy

# Create an EKS cluster against LocalStack
lstk eksctl create cluster --nodes 1

```

## Snapshots

Snapshots capture the running emulator's state so you can restore it later.

A snapshot reference is a **local file**, a **cloud snapshot**, or an **S3 remote**:

- **Local file** — an absolute or relative path. A `.snapshot` extension is added if omitted (snapshots saved as `.zip` by older lstk versions still load).
- **Cloud snapshot** — a name with the `pod:` prefix (e.g. `pod:my-baseline`), stored on the LocalStack platform. Requires authentication (`LOCALSTACK_AUTH_TOKEN` or `lstk login`).
- **S3 remote** — an `s3://bucket/prefix` location backed by your own S3 bucket. Supported by `save`, `load`, and `list`.

```bash
# Save (local, cloud, or S3)
lstk snapshot save ./my-snapshot.snapshot
lstk snapshot save pod:my-baseline
lstk snapshot save my-pod s3://my-bucket/prefix

# Load (starts the emulator first if needed)
lstk snapshot load pod:my-baseline
lstk snapshot load my-pod s3://my-bucket/prefix

# List cloud snapshots — only your own by default, --all for the whole organization
lstk snapshot list
lstk snapshot list --all

# List snapshots in an S3 bucket
lstk snapshot list s3://my-bucket/prefix

# Show metadata for a single cloud snapshot
lstk snapshot show pod:my-baseline

# Remove — cloud snapshots only; local files are never deleted by the CLI
lstk snapshot remove pod:my-baseline          # prompts for confirmation
lstk snapshot remove pod:my-baseline --force  # skip the prompt (required in non-interactive mode)
```

`lstk snapshot load` supports merge strategies via `--merge` (`account-region-merge` (default), `overwrite`, `service-merge`) to control how snapshot state combines with running state.

### S3 remotes

`save`, `load`, and `list` can target your own S3 bucket with an `s3://bucket/prefix` location. The pod name (the snapshot's identity within the bucket) is a positional argument separate from the `s3://` location — required for `load`, auto-generated for `save` when omitted, and unused for `list`.

Credentials come from `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` (and optional `AWS_SESSION_TOKEN`), or from a named profile via `--profile <name>`. **Never put credentials in the URL** — lstk rejects an `s3://` ref that embeds them. lstk itself never touches S3: the running emulator performs the transfer, so these commands require a running emulator, and `list s3://…` queries the emulator rather than the LocalStack platform.

```bash
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...

lstk snapshot save my-pod s3://my-bucket/prefix
lstk snapshot load my-pod s3://my-bucket/prefix
lstk snapshot list s3://my-bucket/prefix

# Or read credentials from a named AWS profile instead of env vars
lstk snapshot save my-pod s3://my-bucket/prefix --profile my-aws-profile
```

The S3 bucket must already exist — lstk checks up front and errors out rather than creating it on a typo. `remove` and `show` are not yet supported for S3 remotes.

### Auto-load on start

The AWS emulator can automatically load a snapshot whenever it starts. Set the `snapshot` field on its `[[containers]]` block to any snapshot reference — a local file or a `pod:` cloud snapshot:

```toml
[[containers]]
type     = "aws"
port     = "4566"
snapshot = "pod:my-baseline"
```

Override or disable it for a single run without editing the config:

```bash
lstk start --snapshot pod:other-baseline  # load a different snapshot this run
lstk start --no-snapshot                  # skip auto-loading this run
```

## Extensions

lstk supports Git-style extensions: running `lstk <name>`, for a name that isn't a built-in command, resolves and delegates to an external `lstk-<name>` executable — checked first in lstk's bundled-extensions directory, then on your `PATH` — forwarding all arguments and passing stdin/stdout/stderr through.

```bash
lstk my-tool --flag  # resolves and runs lstk-my-tool, if it exists
```

Extensions receive context about the current lstk setup (config dir, auth token, running emulators) via environment variables, so they can integrate without reimplementing discovery.

See [docs/extensions-authoring.md](docs/extensions-authoring.md) for the extension contract and how to author your own.

## Reporting bugs

Feedback is welcome! Use the repository issue tracker for bug reports or feature requests.
