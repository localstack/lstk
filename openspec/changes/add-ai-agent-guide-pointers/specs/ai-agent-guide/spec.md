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

### Requirement: AI agents pointer in the start summary
Every successful emulator start that prints the start summary (`• Endpoint`, `• Web app`) SHALL also print `• AI agents: https://www.localstack.cloud/.well-known/agents.md` as the last summary line, in interactive and non-interactive mode alike. It SHALL NOT be a `> Tip:` line and SHALL NOT affect which tip is shown.

#### Scenario: Non-interactive start against a running emulator
- **WHEN** `lstk start --non-interactive` finds the AWS emulator already running
- **THEN** stdout contains `• AI agents: https://www.localstack.cloud/.well-known/agents.md`, and does not contain the post-login guide block
