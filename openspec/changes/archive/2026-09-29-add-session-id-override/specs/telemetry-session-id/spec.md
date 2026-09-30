## Purpose

Defines how lstk chooses the analytics session id stamped on the telemetry events of one invocation, and how a caller can supply that id so lstk's events join the caller's own session.

## ADDED Requirements

### Requirement: Each invocation has one analytics session id

lstk SHALL stamp every analytics event it emits during one invocation with the same `session_id`. When no valid override is supplied, lstk SHALL generate a fresh random id per invocation.

#### Scenario: Default random session id

- **WHEN** telemetry is enabled and `LSTK_SESSION_ID` is unset
- **THEN** every event lstk emits for the invocation carries the same `session_id`
- **AND** a second invocation carries a different `session_id`

### Requirement: Caller can override the session id with LSTK_SESSION_ID

When telemetry is enabled and `LSTK_SESSION_ID` is set to a well-formed value, lstk SHALL use that value, after trimming surrounding whitespace, as the `session_id` on every analytics event it emits for the invocation, instead of a generated one. The value is opaque: lstk SHALL NOT require a particular format such as a UUID, and SHALL reject only clearly malformed values — empty after trimming, containing whitespace or control characters, or longer than 256 characters.

A malformed value SHALL NOT fail the command or change its output or exit code: lstk SHALL fall back to a generated id and record the rejection in its diagnostic log only.

The variable SHALL NOT be described in any user-facing surface: command help, `lstk docs` output, the default config file, or published documentation.

#### Scenario: Override applied to lstk's events

- **WHEN** telemetry is enabled and `LSTK_SESSION_ID=caller-session-123`
- **THEN** every event lstk emits for the invocation carries `session_id` equal to `caller-session-123`

#### Scenario: Surrounding whitespace trimmed

- **WHEN** `LSTK_SESSION_ID` is `caller-session-123` followed by a newline
- **THEN** events carry `session_id` equal to `caller-session-123`

#### Scenario: Malformed override ignored

- **WHEN** `LSTK_SESSION_ID` contains embedded whitespace or a control character, is blank, or is longer than 256 characters
- **THEN** the command's output and exit code are the same as with the variable unset
- **AND** events carry a generated `session_id`, not the supplied value

#### Scenario: Telemetry opt-out wins

- **WHEN** `LOCALSTACK_DISABLE_EVENTS=1` and `LSTK_SESSION_ID` is set
- **THEN** lstk emits no analytics events

#### Scenario: Not mentioned in help

- **WHEN** a user runs `lstk --help` or `lstk docs`
- **THEN** the output does not mention `LSTK_SESSION_ID`

### Requirement: Extensions receive the effective session id

When lstk conveys a session id to an extension through `LSTK_EXT_CONTEXT.sessionId`, that value SHALL be the effective session id of the invocation — the override when one was applied, otherwise the generated id — so it always equals the `session_id` on the `ext:<name>` event lstk records for the same invocation. When telemetry is disabled, `sessionId` SHALL remain absent even if `LSTK_SESSION_ID` is set.

#### Scenario: Override conveyed to extension

- **WHEN** telemetry is enabled, `LSTK_SESSION_ID=caller-session-123`, and lstk dispatches to a resolved extension `deploy`
- **THEN** `LSTK_EXT_CONTEXT.sessionId` is `caller-session-123`
- **AND** the `ext:deploy` event lstk records carries `session_id` equal to `caller-session-123`

#### Scenario: Override not conveyed when telemetry disabled

- **WHEN** `LOCALSTACK_DISABLE_EVENTS=1`, `LSTK_SESSION_ID` is set, and lstk dispatches to an extension
- **THEN** `LSTK_EXT_CONTEXT` has no `sessionId` field
- **AND** the extension still runs and its exit code still propagates
