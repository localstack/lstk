## 1. AI agent guide

- [x] 1.1 Integration tests: device-flow success shows the block, failure does not; non-interactive start shows the `• AI agents:` line (all failed before 1.2/1.3).
- [x] 1.2 Emit the guide block after `Login successful.` (`internal/auth`).
- [x] 1.3 Add the `• AI agents:` line to `emitPostStartPointers` (`internal/container`).
- [x] 1.4 Integration tests: already-logged-in login (interactive, stored token, and non-interactive) shows the block; non-interactive login without a token names `LOCALSTACK_AUTH_TOKEN` and agents.md (all failed before 1.5).
- [x] 1.5 Show the guide on the already-logged-in path and add next steps to the non-interactive login error (`cmd/login.go`).
- [x] 1.6 Integration tests: non-interactive start without a token names agents.md; `start --json` has `data.agentsGuide`; status shows the `• AI agents:` line; only root help carries the URL (all failed before 1.7).
- [x] 1.7 Add next steps to the missing-token start error, `agentsGuide` to the start envelope, the line to the status summary, and the root help footer and `Long`; update affected snapshots and `docs/structured-output.md`.
- [x] 1.8 Replace the root help footer with a `Learn more:` section (docs + agents guide); test first, update help snapshots.
- [x] 1.9 Add Homebrew cask caveats (validated with `goreleaser check` 2.20.3) and the README section.
