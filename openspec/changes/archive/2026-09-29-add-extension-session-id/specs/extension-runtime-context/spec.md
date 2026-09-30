## ADDED Requirements

### Requirement: The telemetry session id is conveyed for correlation

lstk SHALL include its telemetry session id for the current invocation as the `sessionId` field of `LSTK_EXT_CONTEXT`, so an extension that emits its own product telemetry can join it to the `ext:<name>` command event lstk records for the same invocation (see "Extension invocations are recorded in product telemetry"). The conveyed value SHALL be the same session id carried on the events lstk itself emits for that invocation, so the join is exact rather than inferred from a machine identifier and event timestamps.

When lstk's telemetry is disabled, there is no session to correlate: lstk SHALL omit the `sessionId` field rather than conveying an empty value. An extension therefore cannot distinguish a telemetry-disabled lstk from an lstk released before this field existed — both present as absence. This ambiguity is accepted: both cases mean no correlation is available, and an extension SHALL treat absence as such (for example by generating its own identifier) rather than requiring the field.

This field is additive: `LSTK_EXT_API_VERSION` SHALL NOT be incremented for it, and extensions SHALL detect it by presence, per the "Versioned JSON context contract" requirement. lstk SHALL NOT change the events it emits as part of conveying this value, and SHALL NOT require the extension to emit telemetry at all.

#### Scenario: Session id conveyed and matching lstk's own event

- **WHEN** telemetry is enabled and lstk dispatches to a resolved extension `deploy`
- **THEN** `LSTK_EXT_CONTEXT.sessionId` is set to lstk's session id for that invocation
- **AND** it equals the session id carried on the `ext:deploy` command event lstk records for the same invocation

#### Scenario: Session id omitted when telemetry is disabled

- **WHEN** telemetry is disabled and lstk dispatches to an extension
- **THEN** `LSTK_EXT_CONTEXT` has no `sessionId` field (it is absent, not present with an empty value)
- **AND** the extension still runs and its exit code still propagates

#### Scenario: Adding the field does not change the contract version

- **WHEN** an extension built against contract version 1 is invoked by an lstk that conveys `sessionId`
- **THEN** `LSTK_EXT_API_VERSION` is still `1`
- **AND** the extension runs unmodified, ignoring the field it does not know about
