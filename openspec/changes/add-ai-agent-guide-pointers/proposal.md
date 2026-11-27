## Why

LocalStack publishes a setup guide for AI coding agents at https://www.localstack.cloud/.well-known/agents.md (install, authenticate, start, MCP server, skills), and a human-facing page at https://docs.localstack.cloud/aws/getting-started/ai-workflows/. Nothing in lstk's own output points at either, so neither a human setting up an agent nor an agent driving lstk is likely to find them (DEVX-1461).

## What Changes

- After a successful `lstk login` (or a login inside `lstk start`), print a short block modelled on the post-login "Build with your favorite AI agent" panel other CLIs show: a prompt to paste into an agent that names agents.md, and the docs guide.
- Print the same block when `lstk login` finds an existing login, interactive or not. Agents usually run with `LOCALSTACK_AUTH_TOKEN` set, so this is the login path they actually hit.
- When `lstk login` runs non-interactively with no login, keep the error but add next steps: set `LOCALSTACK_AUTH_TOKEN`, or follow agents.md, whose headless-setup section covers getting a token.
- Add an `• AI agents: <agents.md URL>` line to the start summary, after `• Endpoint` and `• Web app`, on every successful start. This is the path that reaches agents: login only runs interactively, while the start summary is printed in non-interactive runs too.

- Point at agents.md from the other places agents look: the missing-token error of a non-interactive start, `data.agentsGuide` in `--json` start output (JSON mode drops the summary line), the `lstk status` summary, and a `Learn more:` section (docs and agents guide) on root `lstk --help` / `lstk docs`.
- At install time: Homebrew cask caveats with the prompt, and a README section (also the npm package page). No npm postinstall message: npm 7+ hides lifecycle script output unless the script fails, and many installs skip scripts entirely.

Considered and not taken: a rotating `> Tip:` (shown only on some starts, and it would compete with the other tips), a footer on root `lstk --help`, a tip shown only when `internal/caller` detects an agent, and an "Agent guide" action on every error. The browser page shown after login belongs to the web app, so a panel there is a web-app change, out of scope here.

## Capabilities

### New Capabilities
- `ai-agent-guide`: the post-login AI agent block and the start-summary pointer.

## Impact

- `internal/auth/auth.go`: `AgentsGuideURL` constant and `emitAIAgentGuide`, emitted after `Login successful.`.
- `cmd/login.go`: the existing-login check moves before the interactivity check; the non-interactive error becomes an `ErrorEvent` with actions.
- `internal/container/status.go`, `internal/output` (`InstanceInfoEvent`, `EmulatorStartedEvent`, envelope): status line and `data.agentsGuide`; `docs/structured-output.md` documents the new field.
- `cmd/help.go`, `cmd/root.go`: root help footer and root `Long`.
- `.goreleaser.yaml` (`homebrew_casks.caveats`), `README.md`.
- `internal/ui/run.go`: `RunMessage` takes several events.
- `internal/container/start.go`: `emitPostStartPointers` adds the `• AI agents:` line.
- Uses existing `MessageEvent` severities, like the neighbouring start pointers; no new event type.
