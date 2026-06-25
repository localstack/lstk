# extension-runtime-context Specification

## Purpose

Define the versioned contract by which lstk passes runtime context — resolved emulator endpoint and type, config directory, auth token, and resolved global-flag state — to an extension process, so extensions can talk to the emulator and the LocalStack platform and honor lstk's global flags without re-implementing discovery, config resolution, or auth.

## ADDED Requirements

### Requirement: Versioned context contract

lstk SHALL pass runtime context to an extension exclusively through environment variables prefixed with `LSTK_EXT_`, and SHALL set `LSTK_EXT_API_VERSION` to the integer version of the contract it implements. The contract SHALL be additive within a major version; removing or repurposing a variable SHALL require incrementing `LSTK_EXT_API_VERSION`. lstk SHALL NOT require the extension to parse lstk's own config files.

#### Scenario: API version is advertised

- **WHEN** lstk invokes any extension
- **THEN** the extension's environment includes `LSTK_EXT_API_VERSION` set to the current contract version

#### Scenario: Extension reads context from environment only

- **WHEN** an extension needs the emulator endpoint and config directory
- **THEN** it can obtain both from `LSTK_EXT_` environment variables without reading lstk's TOML config

### Requirement: Emulator endpoint and type are provided

When a LocalStack emulator is running, lstk SHALL resolve the emulator endpoint using the same discovery and host resolution used by built-in commands, and SHALL expose it to the extension as `LSTK_EXT_EMULATOR_ENDPOINT` (a full URL), along with `LSTK_EXT_EMULATOR_TYPE` (e.g. `aws`, `snowflake`, `azure`) and the port. When no emulator is running, lstk SHALL omit the endpoint variable rather than setting an invalid value.

#### Scenario: Endpoint provided when emulator running

- **WHEN** an AWS emulator is running and lstk invokes an extension
- **THEN** `LSTK_EXT_EMULATOR_ENDPOINT` is set to the resolved emulator URL
- **AND** `LSTK_EXT_EMULATOR_TYPE` is set to `aws`

#### Scenario: Endpoint omitted when no emulator running

- **WHEN** no emulator is running and lstk invokes an extension
- **THEN** `LSTK_EXT_EMULATOR_ENDPOINT` is not set
- **AND** the extension is still executed

### Requirement: Auth token and config directory are provided

When the user is authenticated, lstk SHALL pass the resolved auth token to the extension as `LSTK_EXT_AUTH_TOKEN` so the extension can call the emulator and the LocalStack platform on the user's behalf. lstk SHALL pass the resolved lstk config directory as `LSTK_EXT_CONFIG_DIR`. When no auth token is available, lstk SHALL omit `LSTK_EXT_AUTH_TOKEN` rather than setting an empty value.

#### Scenario: Auth token passed when available

- **WHEN** the user has a resolved auth token and invokes an extension
- **THEN** `LSTK_EXT_AUTH_TOKEN` is set to that token in the extension's environment

#### Scenario: Auth token omitted when unauthenticated

- **WHEN** no auth token can be resolved and lstk invokes an extension
- **THEN** `LSTK_EXT_AUTH_TOKEN` is not set
- **AND** the extension is still executed

#### Scenario: Config directory always provided

- **WHEN** lstk invokes any extension
- **THEN** `LSTK_EXT_CONFIG_DIR` is set to the resolved lstk config directory

### Requirement: Resolved global flags are conveyed

lstk SHALL parse its own global flags (for example `--non-interactive`) when they appear before the extension command name, resolve them, and convey the resulting state to the extension via `LSTK_EXT_` environment variables rather than forwarding the flags on the extension's command line. In particular, lstk SHALL set `LSTK_EXT_NON_INTERACTIVE` to a truthy value when the session is non-interactive (the user passed `--non-interactive` or the standard output is not a TTY). Each lstk global flag that affects runtime behavior SHALL be conveyed as an `LSTK_EXT_` variable; adding a new global-flag variable is an additive change under `LSTK_EXT_API_VERSION`. lstk SHALL NOT include its global flags in the arguments forwarded to the extension.

#### Scenario: Non-interactive flag conveyed via environment

- **WHEN** a user runs `lstk --non-interactive hello --foo` and `lstk-hello` is resolvable
- **THEN** `LSTK_EXT_NON_INTERACTIVE` is set in the extension's environment
- **AND** the extension is invoked with only `--foo` (the `--non-interactive` global flag is not forwarded on its command line)

#### Scenario: Non-interactive inferred from a non-TTY

- **WHEN** lstk invokes an extension and standard output is not a terminal
- **THEN** `LSTK_EXT_NON_INTERACTIVE` is set even if `--non-interactive` was not passed

### Requirement: Host environment is preserved

lstk SHALL pass the user's existing environment through to the extension and only add or override the `LSTK_EXT_` variables defined by this contract, so extensions inherit the user's `PATH`, locale, and tool configuration.

#### Scenario: Existing environment inherited

- **WHEN** the user has `HTTP_PROXY` set and invokes an extension
- **THEN** the extension's environment still contains `HTTP_PROXY`
- **AND** also contains the `LSTK_EXT_` variables
