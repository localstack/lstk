## 1. Integration tests first (red)

- [x] 1.1 Add `TestSessionIDOverrideAppliedToEvents` to `test/integration/telemetry_test.go`: run a command with `mockAnalyticsServer` and `LSTK_SESSION_ID=caller-session-123` (plus a trailing newline variant) and assert every received event's `metadata.session_id` is `caller-session-123`; confirm it fails before implementation
- [x] 1.2 Add `TestSessionIDOverrideMalformedIgnored` (table: embedded space, control char, blank, 257 chars): assert exit code/output match an unset run and `session_id` is a UUID, not the supplied value; confirm it fails where applicable before implementation
- [x] 1.3 Add `TestExtensionSessionIDOverrideConveyed` to `test/integration/extension_test.go`: with `LSTK_SESSION_ID=caller-session-123`, the reference extension echoes `SESSION_ID=caller-session-123` and the `ext:<name>` event carries the same `session_id`; and with `LOCALSTACK_DISABLE_EVENTS=1` no `SESSION_ID=` line appears and the exit code propagates. Confirm the first case fails before implementation

## 2. Validator

- [x] 2.1 Add `validate.SessionID` to `internal/validate/validate.go` (reject empty, whitespace, control chars, > 256 chars; no charset restriction) with rule-code tests in `validate_test.go`; verify with `go test ./internal/validate/`

## 3. Telemetry client option

- [x] 3.1 Add `Option` / `WithSessionID(id string)` and a variadic `opts ...Option` to `telemetry.New` in `internal/telemetry/client.go`; `newClient` uses the id when non-empty, else `uuid.NewString()`; a disabled client stays empty. Doc comment marks it internal and intentionally undocumented
- [x] 3.2 Unit-test in `internal/telemetry/client_test.go`: override is returned by `SessionID()` and stamped on emitted events; empty option falls back to a random UUID; disabled client ignores it. Verify with `go test ./internal/telemetry/`

## 4. Command-boundary wiring

- [x] 4.1 Capture `LSTK_SESSION_ID` raw into a new `Env.SessionID` field in `internal/env/env.go` via `os.Getenv`, with a doc comment noting it is internal-only and deliberately undocumented
- [x] 4.2 In `cmd/root.go`, trim and validate `cfg.SessionID` before `telemetry.New`; pass `telemetry.WithSessionID` when valid, `logger.Error` the rejection otherwise (move logger-dependent ordering if needed so the log is written). Verify the section 1 integration tests now pass with `make test-integration RUN='TestSessionIDOverride|TestExtensionSessionIDOverride'`
- [x] 4.3 Confirm no user-facing mention: `grep -rn LSTK_SESSION_ID docs/ cmd/ internal/config/default_config.toml` returns only code, and `lstk --help` / `lstk docs` output does not contain it

## 5. Final checks

- [x] 5.1 Run `make test`, `make lint`, and the existing telemetry/extension integration tests (`make test-integration RUN='Telemetry|TestExtension'`) and confirm all pass
