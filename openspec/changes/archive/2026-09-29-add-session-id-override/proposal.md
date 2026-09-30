## Why

Some callers run `lstk` as one step of a larger flow that already has its own analytics session (for example another LocalStack tool, a wrapper, or a test harness). Today every `lstk` process picks a fresh random `session_id`, so the caller's events and lstk's cannot be joined, and a flow that runs `lstk` several times shows up as several unrelated sessions.

## What Changes

- **Add an `LSTK_SESSION_ID` environment variable.** When set to a well-formed value and telemetry is enabled, lstk uses it as the `session_id` on every analytics event it emits for that invocation, instead of generating a random one.
- **The overridden id reaches extensions.** The `sessionId` field of `LSTK_EXT_CONTEXT` carries the same effective id, so an extension's own telemetry joins the caller's session too. (The extension also inherits `LSTK_SESSION_ID` itself, since only `LSTK_EXT_*` is stripped from its environment.)
- **Malformed or blank values are ignored**: lstk falls back to a random id and logs the rejection to `lstk.log`. The command never fails because of this variable.
- **Telemetry opt-out still wins**: with `LOCALSTACK_DISABLE_EVENTS=1`, nothing is emitted and no `sessionId` is conveyed, whatever `LSTK_SESSION_ID` says.
- **Internal only, deliberately undocumented**: no entry in `docs/`, `--help`, `lstk docs` output, `default_config.toml`, or the CLAUDE.md environment-variable list. The only description lives in code doc comments.

## Capabilities

### New Capabilities

- `telemetry-session-id`: how lstk chooses the analytics session id for an invocation (random by default, overridable via `LSTK_SESSION_ID`), and that the same id is used on every event and conveyed to extensions.

### Modified Capabilities

None. The `sessionId` context field itself is introduced by the in-flight `add-extension-session-id` change and is not yet in `openspec/specs/extension-runtime-context`; its behaviour (the conveyed value equals the id on lstk's own events) is unchanged — only the source of that id changes, which this new capability covers.

## Impact

- **Code**: `internal/env/env.go` (capture the variable), `internal/validate/validate.go` (a loose `SessionID` validator), `internal/telemetry/client.go` (a `WithSessionID` option on `New`), `cmd/root.go` (resolve, validate, and pass it). `cmd/extension.go` needs no change — it already reads `tel.SessionID()`.
- **Tests**: `internal/validate/validate_test.go`, `internal/telemetry/client_test.go`, `test/integration/telemetry_test.go`, `test/integration/extension_test.go`.
- **Docs**: none, by design.
