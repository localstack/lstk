## Context

`add-extension-mechanism` established the `LSTK_EXT_*` runtime-context contract: two environment variables, one of them a JSON object, with additive fields detected by presence and `LSTK_EXT_API_VERSION` reserved for breaking changes. It also established that lstk records each resolved extension invocation as an `lstk_command` event named `ext:<name>`, and explicitly deferred injecting OpenTelemetry trace context into the extension process.

An extension that emits its own product telemetry sits in the gap this leaves: its events and lstk's describe the same invocation, but nothing links them. This change closes that gap with the smallest possible addition — one string field — and settles two questions: how the value travels, and what happens when there is no value.

## Decisions

### Decision 1: Convey it as a field of the existing JSON context, not a new `LSTK_EXT_*` variable

The session id becomes `sessionId` in `LSTK_EXT_CONTEXT`, alongside `configDir`, `authToken`, `nonInteractive`, and `json`.

**Rationale**: the contract already declares exactly one JSON object as the place resolved runtime context lives, with a stated additive-field mechanism (presence, not version) that this field satisfies with `omitempty`. Extensions already decode that object, so they gain the field by reading one more key. `LSTK_EXT_API_VERSION` stays `1`.

**Alternatives considered**: a dedicated `LSTK_EXT_SESSION_ID` variable (rejected: the flat-variable form is reserved for the version, which exists outside the JSON precisely so it can be read *before* parsing — a session id has no such bootstrapping need, and adding flat variables would fork the contract into two mechanisms with two sets of rules). Passing it as an argument to the extension (rejected: arguments are the extension's own namespace — lstk forwards them verbatim and injects nothing).

### Decision 2: Omit the field when telemetry is disabled, and accept that absence is ambiguous

With telemetry disabled, `telemetry.New` returns a client with an empty session id, and `omitempty` drops the key. An extension therefore cannot distinguish "lstk telemetry is disabled" from "this lstk predates the field" — both are absence. The ambiguity is documented in the author guide and in the requirement rather than engineered away.

**Rationale**: a disabled client has no session, so there is nothing to correlate — conveying an empty string would offer a key that satisfies a naive presence check while joining to nothing. The two indistinguishable cases also call for the *same* handling: no correlation is available, so fall back to a locally generated id. A distinction that changes no behavior is not worth a mechanism.

**Alternatives considered**: minting a session id even when telemetry is disabled (rejected: it manufactures a correlation handle for an invocation that reports nothing, and puts an identifier into the environment of a user who opted out of analytics). Conveying `sessionId: null` or an explicit `telemetryEnabled` boolean to disambiguate (rejected: both add contract surface to answer a question no extension needs to act on; the second also duplicates `LOCALSTACK_DISABLE_EVENTS`, which extensions already inherit, since `Context.Environ` strips only `LSTK_EXT_*`).

### Decision 3: Two spellings for one value, deliberately

The context field is `sessionId`; the analytics wire format calls the same value `session_id`.

**Rationale**: each name follows its own established convention — `LSTK_EXT_CONTEXT` is lowerCamelCase throughout (`configDir`, `authToken`, `nonInteractive`), the analytics payload is snake_case throughout. Harmonising would break one convention to serve a cosmetic consistency across two systems that are never read side by side. The author guide notes the difference so it is not mistaken for a bug.
