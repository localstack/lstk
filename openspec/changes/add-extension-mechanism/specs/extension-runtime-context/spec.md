# extension-runtime-context Specification

## Purpose

Define the versioned contract by which lstk passes runtime context — resolved emulator endpoint and type, config directory, auth token, and resolved global-flag state — to an extension process, so extensions can talk to the emulator and the LocalStack platform and honor lstk's global flags without re-implementing discovery, config resolution, or auth.

## ADDED Requirements

### Requirement: Versioned JSON context contract

lstk SHALL pass runtime context to an extension through exactly two environment variables: `LSTK_EXT_API_VERSION`, set to the integer version of the contract it implements, and `LSTK_EXT_CONTEXT`, a single JSON object carrying the resolved context (config directory, auth token, resolved global-flag state, and running emulators). The version is kept as a flat scalar — outside the JSON payload — so an extension can read it before parsing the object.

The two concerns of detecting *additive* fields and guarding against *breaking* changes are handled separately:

- `LSTK_EXT_API_VERSION` SHALL be incremented **only** when a field is removed or repurposed (a breaking change). Adding a field SHALL NOT increment it. An extension uses the version to refuse a contract generation it does not understand (a version higher than it was built for).
- Additive fields SHALL be detected by an extension through their **presence in the JSON object**, not through the version number — since the version does not change when a field is added, it cannot signal a new field's availability.
- Any field added after version 1 SHALL be distinguishable when absent (omitted, null, or otherwise not a zero value indistinguishable from "not provided"), so an extension running against an older lstk that omits the field can tell it apart from a newer lstk that provides it.

lstk SHALL NOT require the extension to parse lstk's own config files.

#### Scenario: API version is advertised

- **WHEN** lstk invokes any extension
- **THEN** the extension's environment includes `LSTK_EXT_API_VERSION` set to the current contract version

#### Scenario: Context is a single JSON object

- **WHEN** lstk invokes any extension
- **THEN** the extension's environment includes `LSTK_EXT_CONTEXT` containing a JSON object the extension can decode to obtain the config directory, auth token (when present), non-interactive state, and the list of running emulators, without reading lstk's TOML config

#### Scenario: A newly added field is detected by presence, not version

- **WHEN** a later lstk adds a new optional field to `LSTK_EXT_CONTEXT` without incrementing `LSTK_EXT_API_VERSION`
- **THEN** an extension determines the field's availability by checking for its presence in the decoded object
- **AND** an older lstk that omits the field is distinguishable from a newer lstk that provides it

### Requirement: lstk owns the LSTK_EXT_ namespace conveyed to the extension

lstk SHALL strip any inherited `LSTK_EXT_*` variables from the environment it passes to an extension before adding its own, so every `LSTK_EXT_*` variable an extension observes originates from the current lstk invocation rather than the user's shell or a parent process. This holds even though `LSTK_EXT_API_VERSION` and `LSTK_EXT_CONTEXT` are always set (and would override an inherited value anyway): the guarantee extends to `LSTK_EXT_*` names lstk does not set.

#### Scenario: Stray inherited contract variables do not reach the extension

- **WHEN** the environment already contains an `LSTK_EXT_*` variable (e.g. a user-exported `LSTK_EXT_CONTEXT` or a leftover from a nested invocation)
- **THEN** the extension does not observe that inherited value; it sees only the `LSTK_EXT_*` variables lstk resolved for this invocation

### Requirement: Running emulators are provided as a JSON array

The `LSTK_EXT_CONTEXT` object SHALL include an `emulators` array, with one entry per LocalStack emulator currently running, so an extension can work against every running emulator rather than a single one. Each entry SHALL carry the emulator `type` (e.g. `aws`, `snowflake`, `azure`), the `endpoint` (a full URL resolved with the same discovery and host resolution used by built-in commands), and the `port`. When no emulator is running, `emulators` SHALL be an empty array (`[]`), not omitted, so an extension always decodes a list.

#### Scenario: Single emulator provided when one is running

- **WHEN** an AWS emulator is running and lstk invokes an extension
- **THEN** `LSTK_EXT_CONTEXT.emulators` contains one entry with `type` `aws` and `endpoint` set to the resolved emulator URL

#### Scenario: Multiple emulators provided when several are running

- **WHEN** an AWS emulator and a Snowflake emulator are both running and lstk invokes an extension
- **THEN** `LSTK_EXT_CONTEXT.emulators` contains an entry for each, distinguished by `type`

#### Scenario: Empty array when no emulator running

- **WHEN** no emulator is running and lstk invokes an extension
- **THEN** `LSTK_EXT_CONTEXT.emulators` is an empty array
- **AND** the extension is still executed

### Requirement: Auth token and config directory are provided

When the user is authenticated, lstk SHALL include the resolved auth token as the `authToken` field of `LSTK_EXT_CONTEXT` so the extension can call the emulator and the LocalStack platform on the user's behalf. lstk SHALL include the resolved lstk config directory as the `configDir` field. When no auth token is available, lstk SHALL omit the `authToken` field rather than setting an empty value.

#### Scenario: Auth token passed when available

- **WHEN** the user has a resolved auth token and invokes an extension
- **THEN** `LSTK_EXT_CONTEXT.authToken` is set to that token

#### Scenario: Auth token omitted when unauthenticated

- **WHEN** no auth token can be resolved and lstk invokes an extension
- **THEN** `LSTK_EXT_CONTEXT` has no `authToken` field
- **AND** the extension is still executed

#### Scenario: Config directory always provided

- **WHEN** lstk invokes any extension
- **THEN** `LSTK_EXT_CONTEXT.configDir` is set to the resolved lstk config directory

### Requirement: Resolved global flags are conveyed

lstk SHALL parse its own global flags (for example `--non-interactive`) when they appear before the extension command name, resolve them, and convey the resulting state to the extension as fields of `LSTK_EXT_CONTEXT` rather than forwarding the flags on the extension's command line. In particular, lstk SHALL set the `nonInteractive` field to true when the session is non-interactive (the user passed `--non-interactive` or the standard output is not a TTY). Each lstk global flag that affects runtime behavior SHALL be conveyed as a field of the context object; adding a new field is an additive change under `LSTK_EXT_API_VERSION`. lstk SHALL NOT include its global flags in the arguments forwarded to the extension.

#### Scenario: Non-interactive flag conveyed via context

- **WHEN** a user runs `lstk --non-interactive hello --foo` and `lstk-hello` is resolvable
- **THEN** `LSTK_EXT_CONTEXT.nonInteractive` is true
- **AND** the extension is invoked with only `--foo` (the `--non-interactive` global flag is not forwarded on its command line)

#### Scenario: Non-interactive inferred from a non-TTY

- **WHEN** lstk invokes an extension and standard output is not a terminal
- **THEN** `LSTK_EXT_CONTEXT.nonInteractive` is true even if `--non-interactive` was not passed

### Requirement: Host environment is preserved

lstk SHALL pass the user's existing environment through to the extension and only add or override the `LSTK_EXT_` variables defined by this contract, so extensions inherit the user's `PATH`, locale, and tool configuration.

#### Scenario: Existing environment inherited

- **WHEN** the user has `HTTP_PROXY` set and invokes an extension
- **THEN** the extension's environment still contains `HTTP_PROXY`
- **AND** also contains the `LSTK_EXT_API_VERSION` and `LSTK_EXT_CONTEXT` variables

### Requirement: Extension invocations are recorded as telemetry

When lstk telemetry is enabled (the existing `LSTK_OTEL` path), lstk SHALL record each extension invocation — at least the extension command name, the duration, and the exit code — through the same OpenTelemetry export used by the rest of lstk. Recording SHALL respect the same opt-in/opt-out as all lstk telemetry: when telemetry is disabled, lstk SHALL emit nothing for the invocation. lstk SHALL NOT inject trace context into the extension process in this change (an extension's own spans nesting under lstk's trace is deferred and would be an additive change).

#### Scenario: Invocation recorded when telemetry enabled

- **WHEN** telemetry is enabled and lstk dispatches to an extension that exits with a status code
- **THEN** lstk records the extension's command name, duration, and exit code via its telemetry export

#### Scenario: Nothing recorded when telemetry disabled

- **WHEN** telemetry is disabled and lstk dispatches to an extension
- **THEN** lstk emits no telemetry for the invocation
- **AND** the extension still runs and its exit code still propagates
