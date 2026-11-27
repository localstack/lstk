## ADDED Requirements

### Requirement: AI agent guide after login
After a successful login, the system SHALL print, below `Login successful.`, a blank line followed by exactly these lines, each at most 80 columns wide:

```
Build with your favorite AI agent
• Paste this prompt into your agent:
  Set up LocalStack using https://www.localstack.cloud/.well-known/agents.md
• Guide: https://docs.localstack.cloud/aws/getting-started/ai-workflows/
```

A failed or cancelled login SHALL NOT print the block.

#### Scenario: Successful device login
- **WHEN** `lstk login` completes the browser device flow successfully
- **THEN** the output contains the AI agent guide block after `Login successful.`

#### Scenario: Failed device login
- **WHEN** the auth request is not confirmed and `lstk login` fails
- **THEN** the output does not contain `Build with your favorite AI agent`

### Requirement: AI agent guide when already logged in
When `lstk login` finds an existing login (`LOCALSTACK_AUTH_TOKEN` set, or a stored token), it SHALL print `You're already logged in` followed by the same AI agent guide block and exit 0, in interactive and non-interactive mode alike. The existing login SHALL be checked before interactivity, so an agent running with a token and no terminal gets the guide rather than an error.

#### Scenario: Non-interactive login with a token set
- **WHEN** `lstk login --non-interactive` runs with `LOCALSTACK_AUTH_TOKEN` set
- **THEN** it exits 0 and stdout contains `You're already logged in` and the AI agent guide block

#### Scenario: Interactive login with a stored token
- **WHEN** `lstk login` runs in a terminal with a stored token
- **THEN** it shows `You're already logged in` and the AI agent guide block, and does not start the browser flow

### Requirement: Non-interactive login error points at agent setup
When `lstk login` runs non-interactively with no existing login, it SHALL fail with exit code 1 and the error `login requires an interactive terminal`, listing as next steps setting `LOCALSTACK_AUTH_TOKEN` and the agents guide URL, whose headless-setup section explains how an agent obtains a token.

#### Scenario: Agent runs login without a token
- **WHEN** `lstk login --non-interactive` runs with no token set or stored
- **THEN** it exits 1 and the output contains `login requires an interactive terminal`, `LOCALSTACK_AUTH_TOKEN`, and `https://www.localstack.cloud/.well-known/agents.md`

### Requirement: Missing-token start errors point at agent setup
When a non-interactive `lstk start` or bare `lstk` fails because no auth token is available, the `authentication required` error SHALL list as next steps setting `LOCALSTACK_AUTH_TOKEN` and the agents guide URL. An interactive start's login failures SHALL NOT carry these next steps.

#### Scenario: Agent starts without a token
- **WHEN** `lstk start --non-interactive` runs with no token set or stored
- **THEN** it exits 1 and the output contains `authentication required` and `https://www.localstack.cloud/.well-known/agents.md`

### Requirement: AI agents pointer in JSON start output
The `--json` envelope of `lstk start` and bare `lstk` SHALL carry `data.agentsGuide` set to the agents guide URL, since JSON mode drops the plain-text summary lines.

#### Scenario: JSON start against a running emulator
- **WHEN** `lstk start --non-interactive --json` finds the AWS emulator already running
- **THEN** `data.agentsGuide` is `https://www.localstack.cloud/.well-known/agents.md`

### Requirement: AI agents pointer in status
The running-emulator summary of `lstk status` SHALL end with `• AI agents: https://www.localstack.cloud/.well-known/agents.md`, for Docker-managed emulators and `--endpoint-url` targets alike.

#### Scenario: Status of an external endpoint
- **WHEN** `lstk --endpoint-url <url> status` reaches a running AWS emulator
- **THEN** stdout contains `• AI agents: https://www.localstack.cloud/.well-known/agents.md`

### Requirement: Learn more section in root help
Root `lstk --help` SHALL end with a `Learn more:` section listing `Documentation` (the lstk docs URL) and `AI agents` (the agents guide URL), and the root command's long description (rendered by `lstk docs`) SHALL include both links. Subcommand help SHALL NOT carry the section.

#### Scenario: Root and subcommand help
- **WHEN** `lstk --help` and `lstk start --help` run
- **THEN** only the root help contains the agents guide URL

### Requirement: Install-time pointers
The Homebrew cask SHALL carry caveats with the same prompt and guide link as the post-login block, so `brew install` and `brew info` show them. The README, which is also the npm package page, SHALL have a section on using lstk with AI coding agents containing the prompt.

#### Scenario: Release config is valid
- **WHEN** `goreleaser check` runs on `.goreleaser.yaml`
- **THEN** the config, including `homebrew_casks.caveats`, validates

### Requirement: AI agents pointer in the start summary
Every successful emulator start that prints the start summary (`• Endpoint`, `• Web app`) SHALL also print `• AI agents: https://www.localstack.cloud/.well-known/agents.md` as the last summary line, in interactive and non-interactive mode alike. It SHALL NOT be a `> Tip:` line and SHALL NOT affect which tip is shown.

#### Scenario: Non-interactive start against a running emulator
- **WHEN** `lstk start --non-interactive` finds the AWS emulator already running
- **THEN** stdout contains `• AI agents: https://www.localstack.cloud/.well-known/agents.md`, and does not contain the post-login guide block
