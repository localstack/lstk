## Why

`add-extension-session-id` gave extensions the one thing they needed to join their telemetry to lstk's. Two more facts about an invocation are still trapped inside lstk, and the `doctor` extension needs both.

The first is the endpoint URL. When a user runs `LSTK_ENDPOINT_URL=http://localhost:4566 lstk doctor`, the extension has no way to learn that: `emulators` reports what local Docker discovery found, which is a different question and is frequently empty in exactly the setups an endpoint URL is used for (docker compose, host networking, CI, another machine, a cloud-hosted ephemeral instance). Without the URL the doctor cannot check the host at all — including the DNS names AWS SDKs derive from it (`bucket.s3.<host>`, `sync-<host>`), which is how a plain `localhost` endpoint silently breaks Step Functions' `StartSyncExecution`.

The second is the machine id. The doctor emits its own telemetry and currently re-derives the machine identity itself, repeating lstk's Docker `Info` round-trip. That call can fail, and when it lands on a different fallback than lstk did, one machine reports as two — defeating the correlation `sessionId` was added to provide.

## What Changes

- **Add an `endpointUrl` field to `LSTK_EXT_CONTEXT`**, carrying the endpoint source lstk resolved for this invocation (`--endpoint-url`, then `LSTK_ENDPOINT_URL`, then `AWS_ENDPOINT_URL`). lstk applies its precedence and conveys the winner **verbatim** — unvalidated, unnormalized, unprobed.
- **Omit `endpointUrl` when no endpoint source was set**, i.e. when the invocation targets the default local emulator.
- **Add a `machineId` field to `LSTK_EXT_CONTEXT`**, carrying the anonymized machine identity lstk stamps on its own events — the prepared hash, never a raw Docker or system id.
- **Omit `machineId` when lstk's telemetry is disabled**, alongside `sessionId`: a disabled client computes neither, so the two appear and disappear together. Absence stays ambiguous in the way `add-extension-session-id` already accepted and documented.
- **Do not reject an endpoint URL on the dispatch path.** `rejectEndpointURL` guards built-ins that have no remote equivalent; dispatch makes no such judgment, because what an endpoint target means is the extension's business.
- **Leave `emulators` untouched.** "What is running locally" and "what was I told to target" are separate questions with separate consumers.
- **Keep `LSTK_EXT_API_VERSION` at `1`.** Both additions are additive by definition, detected by presence like every post-v1 field.
- lstk's own telemetry events and endpoint handling are unchanged: no new event, no new field on the existing one, no change to how any built-in resolves an endpoint. The machine-id *derivation* does change shape — it moves into the `MachineID(ctx)` accessor, is skipped entirely when telemetry is disabled, and is bounded by a 3s timeout so an unreachable Docker daemon cannot stall dispatch (design.md, Decision 2) — but the derived value and the events carrying it are the same.

## Capabilities

### Modified Capabilities

- `extension-runtime-context`: gains a requirement that lstk conveys the resolved endpoint source as the `endpointUrl` field, verbatim and unvalidated, present when a source was set and omitted otherwise; and a requirement that lstk conveys its anonymized machine identity as the `machineId` field, equal to the value on its own events and omitted when telemetry is disabled. No existing requirement changes.

## Impact

- **Touched code**: `internal/telemetry` (a `MachineID(ctx)` accessor owning the lazy machine-id derivation — enabled-guarded before any computation, Docker lookup bounded by a 3s timeout — with `GetEnvironment` reading through it), `internal/extension/context.go` (the two new fields), `cmd/extension.go` (populate them), `cmd/root.go` (read the endpoint source at the call site, which is the only scope holding the `*cobra.Command` the flag lives on, and pass it to `dispatchExtension`). No change to `internal/endpoint`: `ResolvedSource` already does exactly what conveying the raw value requires.
- **Tests**: `internal/telemetry/client_test.go`, `internal/extension/extension_test.go`, the reference extension (`test/integration/test-samples/extensions/lstk-ref`) which must echo both fields for them to be observable, and `test/integration/extension_test.go`.
- **Docs**: `docs/extensions-authoring.md` (JSON sample, field table, a "Targeting an external emulator" section, and the machine id folded into the existing telemetry-correlation section) and the CLAUDE.md Extensions section.
- **Consumers**: the `doctor` extension in `lstk-bundled-extensions` detects both fields by presence and falls back cleanly when absent, so the two repos do not block each other and can land in either order.
