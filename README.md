# lstk

**A command-line interface for LocalStack**. Built in Go with a modern terminal UI and native CLI experience for managing and interacting with LocalStack deployments. 👾

```bash
npm install -g @localstack/lstk
```

See [Installation](#installation) for other install methods.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) — required as a container engine.
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
- **Browser-based login** — authenticate via browser and store credentials securely in the system keyring, or use `LOCALSTACK_AUTH_TOKEN` for CI
- **Snapshots** — save, load, and manage emulator state as local files, cloud snapshots, or in your own S3 bucket
- **Cloud CLI proxies** — run `aws`, `az`, `terraform`, `cdk`, and `sam` commands against LocalStack with the endpoint, credentials, and region pre-configured
- **Extensions** — Git-style `lstk-<name>` executables extend the CLI with new commands; see [docs/extensions-authoring.md](docs/extensions-authoring.md)
- **Self-update** — `lstk update` checks for and installs the latest release
- **Structured JSON output** — pass `--json` to a supported command for a machine-readable envelope instead of formatted text; see [docs/structured-output.md](docs/structured-output.md)

For the full command reference, configuration options, environment variables, and troubleshooting, see the **[lstk documentation](https://docs.localstack.cloud/aws/developer-tools/running-localstack/lstk/)**.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, architecture, and pull request guidelines.

## Reporting bugs

Feedback is welcome! Use the [repository issue tracker](https://github.com/localstack/lstk/issues) for bug reports or feature requests.
