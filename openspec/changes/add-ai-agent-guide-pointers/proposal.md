## Why

LocalStack publishes a setup guide for AI coding agents at https://www.localstack.cloud/.well-known/agents.md (install, authenticate, start, MCP server, skills), and a human-facing page at https://docs.localstack.cloud/aws/getting-started/ai-workflows/. Nothing in lstk's own output points at either, so neither a human setting up an agent nor an agent driving lstk is likely to find them (DEVX-1461).

## What Changes

- After a successful `lstk login` (or a login inside `lstk start`), print a short block modelled on the post-login "Build with your favorite AI agent" panel other CLIs show: a prompt to paste into an agent that names agents.md, and the docs guide.
- Add an `• AI agents: <agents.md URL>` line to the start summary, after `• Endpoint` and `• Web app`, on every successful start. This is the path that reaches agents: login only runs interactively, while the start summary is printed in non-interactive runs too.

Considered and not taken: a rotating `> Tip:` (shown only on some starts, and it would compete with the other tips), a footer on root `lstk --help`, a tip shown only when `internal/caller` detects an agent, and an "Agent guide" action on every error. The browser page shown after login belongs to the web app, so a panel there is a web-app change, out of scope here.

## Capabilities

### New Capabilities
- `ai-agent-guide`: the post-login AI agent block and the start-summary pointer.

## Impact

- `internal/auth/auth.go`: `AgentsGuideURL` constant and `emitAIAgentGuide`, emitted after `Login successful.`.
- `internal/container/start.go`: `emitPostStartPointers` adds the `• AI agents:` line.
- Uses existing `MessageEvent` severities, like the neighbouring start pointers; no new event type.
