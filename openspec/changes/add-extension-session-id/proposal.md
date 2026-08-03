## Why

lstk records every extension invocation as an `lstk_command` telemetry event named `ext:<name>`, carrying the invocation's duration and exit code. That event is all lstk can know: an extension is a separate OS process, and the detail that matters for some extensions lives only inside it. The `doctor` extension is the first case — it wants to report its per-check diagnostic results, which lstk's event cannot contain.

Two events then describe one invocation, with no way to join them. lstk's telemetry session id is generated per `telemetry.Client`, i.e. per process, so an extension that builds its own client invents an unrelated id. The only join available today is `machine_id` plus client-time proximity, which is fuzzy in general and wrong outright when several runs land in the same second on one host (CI).

Conveying lstk's session id through the runtime context makes the join exact, and does it once for **every** bundled extension rather than once per extension.

## What Changes

- **Add a `sessionId` field to `LSTK_EXT_CONTEXT`**, carrying lstk's telemetry session id for the current invocation — the same value stamped on the `ext:<name>` event lstk emits for it.
- **Omit the field when lstk's telemetry is disabled**, since a disabled client has no session to correlate. Absence is therefore ambiguous to an extension (telemetry-disabled lstk vs. an lstk predating the field); this is accepted and documented rather than worked around.
- **Keep `LSTK_EXT_API_VERSION` at `1`.** The addition is additive by definition, detected by presence like every post-v1 field.
- lstk's own telemetry is unchanged: no new event, no new field on the existing one.

## Capabilities

### Modified Capabilities

- `extension-runtime-context`: gains a requirement that lstk conveys its telemetry session id as the `sessionId` field, present when telemetry is enabled and omitted when it is disabled, so an extension's own telemetry can be joined to lstk's `ext:<name>` event for the same invocation. No existing requirement changes.

## Impact

- **Touched code**: `internal/telemetry` (a `SessionID()` getter — the field was unexported with no accessor), `internal/extension/context.go` (the new field), `cmd/extension.go` (populate it; `dispatchExtension` already receives the telemetry client, so no signature changes).
- **Tests**: `internal/telemetry/client_test.go`, `internal/extension/extension_test.go`, the reference extension (`test/integration/test-samples/extensions/lstk-ref`) which must echo the field for it to be observable, and `test/integration/extension_test.go`.
- **Docs**: `docs/extensions-authoring.md` (JSON sample, field table, and a section on correlating an extension's telemetry with lstk's) and the CLAUDE.md Extensions section.
- **Consumers**: the `doctor` extension in `lstk-bundled-extensions` treats the field as optional and falls back to a locally generated id, so the two repos do not block each other and can land in either order.
