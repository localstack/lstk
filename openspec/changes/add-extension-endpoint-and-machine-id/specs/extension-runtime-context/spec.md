## ADDED Requirements

### Requirement: The resolved endpoint URL is conveyed verbatim

lstk SHALL include the endpoint URL it was asked to target for the current invocation as the `endpointUrl` field of `LSTK_EXT_CONTEXT`, resolved with lstk's own source precedence: the `--endpoint-url` flag, then `LSTK_ENDPOINT_URL`, then `AWS_ENDPOINT_URL`. Applying that precedence is the only processing lstk performs: it SHALL convey the winning value verbatim and SHALL NOT validate, normalize, or probe it. A malformed or unreachable value SHALL reach the extension unchanged and SHALL NOT fail the dispatch, so that an extension diagnosing a broken endpoint can observe exactly what the user set.

When no endpoint source is set the invocation targets the default local emulator: lstk SHALL omit the `endpointUrl` field rather than conveying an empty value. This field is independent of `emulators`, which continues to report emulators found by local Docker discovery; lstk SHALL NOT derive one from the other, and SHALL NOT reject the dispatch because an endpoint source is set.

This field is additive: `LSTK_EXT_API_VERSION` SHALL NOT be incremented for it, and extensions SHALL detect it by presence, per the "Versioned JSON context contract" requirement.

#### Scenario: Endpoint URL conveyed from each source, in precedence order

- **WHEN** `lstk --endpoint-url http://localhost:4566 deploy` is invoked
- **THEN** `LSTK_EXT_CONTEXT.endpointUrl` is `http://localhost:4566`
- **AND** the same value is conveyed when it comes from `LSTK_ENDPOINT_URL` or from `AWS_ENDPOINT_URL` instead
- **AND** when more than one source is set, the flag wins over `LSTK_ENDPOINT_URL`, which wins over `AWS_ENDPOINT_URL`

#### Scenario: An unusable value is conveyed unchanged rather than rejected

- **WHEN** the resolved endpoint source holds a value that is malformed, or well-formed but unreachable
- **THEN** `LSTK_EXT_CONTEXT.endpointUrl` holds that value byte for byte, with no normalization applied
- **AND** the extension still runs and its exit code still propagates

#### Scenario: Endpoint URL omitted when no source is set

- **WHEN** no endpoint flag or environment variable is set and lstk dispatches to an extension
- **THEN** `LSTK_EXT_CONTEXT` has no `endpointUrl` field (it is absent, not present with an empty value)
- **AND** `emulators` still reports emulators discovered through local Docker, unaffected

#### Scenario: Adding the field does not change the contract version

- **WHEN** an extension built against contract version 1 is invoked by an lstk that conveys `endpointUrl`
- **THEN** `LSTK_EXT_API_VERSION` is still `1`
- **AND** the extension runs unmodified, ignoring the field it does not know about

### Requirement: The anonymized machine id is conveyed for correlation

lstk SHALL include its anonymized machine identity as the `machineId` field of `LSTK_EXT_CONTEXT`, so an extension that emits its own product telemetry reports the same machine as lstk without re-deriving it. The conveyed value SHALL be the identifier lstk stamps on the events it emits for that invocation — the identity check is normative, not incidental: an independently-derived but equal-looking value does not satisfy this requirement, because the derivation can fall back differently and report one machine as two.

lstk SHALL convey the prepared, anonymized value (the salted hash) and SHALL NOT convey the underlying Docker daemon id, `/etc/machine-id`, or any other raw machine identifier.

When lstk's telemetry is disabled it computes no machine identity: lstk SHALL omit the `machineId` field rather than conveying an empty value. `machineId` and `sessionId` are therefore absent together, never one without the other. As with `sessionId`, an extension cannot distinguish a telemetry-disabled lstk from an lstk released before this field existed, and SHALL treat absence as "no shared identity available" — deriving its own if it needs one — rather than requiring the field.

This field is additive: `LSTK_EXT_API_VERSION` SHALL NOT be incremented for it, and extensions SHALL detect it by presence, per the "Versioned JSON context contract" requirement. lstk SHALL NOT change the events it emits as part of conveying this value, and SHALL NOT require the extension to emit telemetry at all.

#### Scenario: Machine id conveyed and matching lstk's own event

- **WHEN** telemetry is enabled and lstk dispatches to a resolved extension `deploy`
- **THEN** `LSTK_EXT_CONTEXT.machineId` is set to lstk's anonymized machine identity
- **AND** it equals the machine id carried on the `ext:deploy` command event lstk records for the same invocation

#### Scenario: Machine id omitted when telemetry is disabled

- **WHEN** telemetry is disabled and lstk dispatches to an extension
- **THEN** `LSTK_EXT_CONTEXT` has no `machineId` field (it is absent, not present with an empty value)
- **AND** `sessionId` is absent as well, since a disabled client computes neither
- **AND** the extension still runs and its exit code still propagates

#### Scenario: Adding the field does not change the contract version

- **WHEN** an extension built against contract version 1 is invoked by an lstk that conveys `machineId`
- **THEN** `LSTK_EXT_API_VERSION` is still `1`
- **AND** the extension runs unmodified, ignoring the field it does not know about
