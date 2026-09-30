## 1. Expose the session id

- [x] 1.1 Add a `SessionID()` getter to `telemetry.Client` (`internal/telemetry/client.go`) — the field is unexported with no accessor, so nothing outside the package can read it; document that it is empty when telemetry is disabled
- [x] 1.2 Unit-test that the getter returns the same value stamped on emitted events and is empty for a disabled client (`internal/telemetry/client_test.go`)

## 2. Add and populate the contract field

- [x] 2.1 Add `SessionID string \`json:"sessionId,omitempty"\`` to `extension.Context` (`internal/extension/context.go`); `omitempty` is required, not stylistic — post-v1 fields are detected by presence. Extend the type's doc comment with what the field is, that it is omitted when telemetry is disabled, and that absence is ambiguous
- [x] 2.2 Populate it in `dispatchExtension` from the `*telemetry.Client` it already receives (`cmd/extension.go`) — no signature changes anywhere
- [x] 2.3 Leave `extension.APIVersion` at `1`: adding a field is additive and must not bump the contract version
- [x] 2.4 Unit-test the round-trip and the omission of the key when the session id is empty (`internal/extension/extension_test.go`)

## 3. Make the field observable end to end

- [x] 3.1 Mirror the field in the reference extension's context struct and echo it as `SESSION_ID=`, omitted when empty like `AUTH_TOKEN` (`test/integration/test-samples/extensions/lstk-ref/main.go`) — without this the integration tests cannot observe the field
- [x] 3.2 Integration test: the conveyed `sessionId` equals `metadata.session_id` on the `lstk_command` event lstk emits for the same invocation (mock analytics server), not merely that it is a UUID
- [x] 3.3 Integration test: with `LOCALSTACK_DISABLE_EVENTS=1` no session id is conveyed, and the extension still runs and still propagates its exit code

## 4. Documentation

- [x] 4.1 `docs/extensions-authoring.md`: add the field to the JSON sample and the field table, and add a "Correlating your telemetry with lstk's" section covering the `sessionId`/`session_id` spelling difference and the ambiguity of absence
- [x] 4.2 Update the CLAUDE.md Extensions section's inline context-field list
