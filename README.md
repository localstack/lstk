# lstk

**A command-line interface for LocalStack**. Built in Go with a modern terminal UI and native CLI experience for managing and interacting with LocalStack deployments. 👾

```bash
npm install -g @localstack/lstk
```

See [Installation](#installation) for other install methods.

## Prerequisites

- A Docker-API-compatible container engine — [Docker](https://docs.docker.com/get-docker/), Rancher Desktop, Colima, OrbStack, Lima, and Podman are all auto-detected; see [container runtimes](https://github.com/localstack/lstk/blob/main/docs/container-runtimes.md) for details.
- [LocalStack account](https://app.localstack.cloud) — required for credentials, the CLI will guide you through authentication.

## Installation

### Homebrew (macOS / Linux)

```bash
brew install localstack/tap/lstk
```

### npm

```bash
npm install -g @localstack/lstk
```

### Binaries

Pre-built binaries are also available from [GitHub Releases](https://github.com/localstack/lstk/releases). 📦

## Quick Start

```sh
lstk
```

Running `lstk` will automatically handle authentication, configuration, and container setup, then start LocalStack. On the first interactive run, it also prompts you to pick which emulator to run (AWS, Azure, or Snowflake) and remembers your choice.

## Features

- **Start / stop / status / logs** — manage the full LocalStack emulator lifecycle with a single command
- **Interactive TUI** — a Bubble Tea-powered terminal UI in interactive terminals, plain output for CI/CD and scripting
- **Browser-based login** — authenticate via browser and store credentials securely in the system keyring, or use `LOCALSTACK_AUTH_TOKEN` for CI (it takes precedence over stored credentials)
- **Snapshots** — save, load, and manage emulator state as local files, cloud snapshots, or in your own S3 bucket
- **Cloud CLI proxies** — run `aws`, `az`, `terraform`, `cdk`, and `sam` commands against LocalStack with the endpoint, credentials, and region pre-configured
- **Target an external emulator** — pass `--endpoint-url <url>` (or set `LSTK_ENDPOINT_URL`) to point most commands at an already-running LocalStack instance — docker compose, host-network mode, CI, a different machine, or a cloud-hosted ephemeral instance (`https://` is supported) — instead of one lstk manages locally
- **Extensions** — Git-style `lstk-<name>` executables extend the CLI with new commands; see [extension authoring](https://github.com/localstack/lstk/blob/main/docs/extensions-authoring.md)
- **Self-update** — `lstk update` checks for and installs the latest release. The automatic check on start is configurable via `[cli] update_check` (`prompt` / `notify` / `off`) or `LSTK_UPDATE_CHECK`; installs managed by mise, asdf, Nix, Scoop or Chocolatey are only ever notified about and left to their own manager
- **Structured JSON output** — pass `--json` to a supported command for a machine-readable envelope instead of formatted text; see [structured output](https://github.com/localstack/lstk/blob/main/docs/structured-output.md)

For the full command reference, configuration options, environment variables, and troubleshooting, see the **[lstk documentation](https://docs.localstack.cloud/aws/developer-tools/running-localstack/lstk/)**.

## Participating

lstk is developed by the LocalStack team. You can read the source, build it, and fork it freely — but we don't accept pull requests from outside collaborators.

The best way to participate is to open a well-formed issue. A bug report we can reproduce is worth more to us than a patch, because it captures the part we can't discover ourselves. [CONTRIBUTING.md](CONTRIBUTING.md) explains what to include in bug reports and feature requests.
