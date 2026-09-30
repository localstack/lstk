## Context

`telemetry.New(endpoint, disabled)` is called once in `cmd/root.go` and generates the session id with `uuid.NewString()` inside `newClient`. A disabled client has an empty id. `dispatchExtension` (`cmd/extension.go`) already conveys `tel.SessionID()` as `LSTK_EXT_CONTEXT.sessionId`, so whatever id the client holds reaches extensions with no further wiring. Environment variables are captured once into `env.Env` by `env.Init()`; `telemetry` imports no config/env package and should stay that way.

## Goals / Non-Goals

**Goals:**
- One place decides the session id; lstk's events and the extension context cannot disagree.
- A bad value can never break a command.

**Non-Goals:**
- Any user-facing documentation or help text.
- A config-file key or CLI flag for the same thing.
- Changing the `sessionId` context contract or `LSTK_EXT_API_VERSION`.
- Propagating the id to the emulator container.

## Decisions

### Decision 1: Override the id inside the telemetry client via an option

`telemetry.New(endpoint, disabled, opts ...Option)` gains `telemetry.WithSessionID(id)`. `newClient` uses the supplied id when non-empty and falls back to `uuid.NewString()` otherwise; a disabled client ignores it and stays empty.

**Rationale**: keeping the id inside the client means `Emit` and `SessionID()` read the same field, so the "extension gets the same id as lstk's events" guarantee holds by construction and needs no change in `cmd/extension.go`. A variadic option leaves the ~10 existing `telemetry.New("", true)` test call sites untouched.

**Alternatives considered**: a `SetSessionID` setter like `SetAuthToken` (rejected: it opens a window where events are emitted with the random id before the setter runs); a new positional parameter (rejected: churns every call site for an internal knob); reading `os.Getenv` inside `internal/telemetry` (rejected: domain packages take values from the command boundary, per the config/env conventions).

### Decision 2: Capture in `env.Env`, validate at the command boundary

`env.Init()` captures `LSTK_SESSION_ID` raw into a new `Env.SessionID` field (via `os.Getenv`, like the other captures that must survive `config.loadConfig`'s `viper.Reset()`). `cmd/root.go` trims it, runs `validate.SessionID`, and passes it via `WithSessionID` only when valid; a rejection is logged with `logger.Error` and otherwise ignored.

**Rationale**: matches how `LOCALSTACK_AUTH_TOKEN` is trimmed and validated in the same function, and keeps `telemetry` free of env/validation concerns.

### Decision 3: Loose opaque-value validation, and fall back rather than fail

A new `validate.SessionID` rejects empty, whitespace, control characters, and length > 256 — the `validate.AuthToken` style for opaque values, with no charset or UUID requirement. On rejection lstk silently (log-only) uses a random id.

**Rationale**: the caller's id format is theirs (it may not be a UUID), so only values that could corrupt the payload or a log line are refused. The feature is undocumented and serves analytics only, so a bad value must never turn into a user-visible error or exit code — the cost of falling back is one uncorrelated session, which is the status quo.

**Alternatives considered**: requiring a UUID (rejected: couples lstk to a caller's id scheme without a backend requirement for it); hard-failing on a malformed value (rejected: an internal analytics knob would then be able to break user commands, and the error would have to name an undocumented variable).

### Decision 4: No documentation surface, but a doc comment

The only description is the doc comment on `Env.SessionID` / `WithSessionID`, stating it is internal and intentionally undocumented so a future contributor does not "fix" the missing docs. The env var is read with `os.Getenv`, not through a Cobra flag or viper default, so it cannot appear in generated help or `lstk docs`.

## Risks / Trade-offs

- [A caller reuses one id across unrelated runs, merging sessions in analytics] → Accepted; that is the caller's intent and responsibility, and the variable is internal.
- [Undocumented behaviour is easy to forget or remove] → Doc comment plus integration tests pin it.
- [The extension also inherits the raw `LSTK_SESSION_ID`, which may be malformed while `sessionId` is the random fallback] → Extensions are told to use `sessionId`; the raw variable passing through is incidental and matches the existing "host environment is preserved" rule. Nested `lstk` calls from an extension will re-validate it the same way.
