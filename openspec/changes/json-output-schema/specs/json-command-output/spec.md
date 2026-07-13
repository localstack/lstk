## ADDED Requirements

### Requirement: Per-command JSON opt-in
A command SHALL only accept `--json` if it carries the JSON-capable annotation. The following commands SHALL carry it as part of this change: `start` (including the root command's own bare, no-subcommand invocation, which shares `start`'s behavior), `stop`, `restart`, `status`, `logs`, `config path`, `volume path`, `volume clear`, `setup aws`, `setup azure`, `az start-interception`, `az stop-interception`, `logout`, `reset`, `snapshot save`, `snapshot load`, `snapshot list`, `snapshot show`, `snapshot remove`, and `update`. `login` and `config profile` SHALL NOT carry it, since both require an interactive terminal unconditionally and have no defined JSON behavior. The `-v`/`--version` flag SHALL NOT support `--json` either, and no subcommand SHALL be added to work around that — it is handled by Cobra before lstk's JSON dispatch runs at all, and this is accepted as a permanent limitation (see design.md's Decisions).

#### Scenario: An opted-in command accepts --json
- **WHEN** `lstk status --json` is run
- **THEN** the command executes normally and emits a JSON envelope instead of being rejected

#### Scenario: login remains JSON-incapable
- **WHEN** `lstk login --json` is run
- **THEN** it is rejected the same way as any other command lacking the JSON-capable annotation (see the `json-flag` capability), naming `login`

#### Scenario: config profile remains JSON-incapable
- **WHEN** `lstk config profile --json` is run
- **THEN** it is rejected the same way, naming `config profile`

#### Scenario: The bare default invocation accepts --json
- **WHEN** `lstk --json` is run with no subcommand
- **THEN** it runs the same start behavior as `lstk start --json` and emits a JSON envelope, rather than being rejected as JSON-incapable

#### Scenario: The --version flag stays plain text under --json, without rejection
- **WHEN** `lstk --version --json` or `lstk -v --json` is run
- **THEN** lstk prints the plain-text version line exactly as it does today
- **AND** it does not render a JSON envelope and does not render the `NOT_JSON_CAPABLE` rejection either, since Cobra's version handling returns before any of lstk's own command dispatch — including the JSON-capability check itself — ever runs

### Requirement: Emulator lifecycle commands report per-emulator results
`start`, `stop`, `restart`, and `status` SHALL report results as an array with one entry per emulator type configured (`internal/config.ContainerConfig`), reflecting that lstk can run more than one emulator type concurrently. `status` SHALL include every configured emulator in its array regardless of whether it is running, rather than stopping at the first one found not running.

#### Scenario: status reports all configured emulators, running or not
- **WHEN** `lstk status --json` is run with an AWS emulator configured and stopped, and a Snowflake emulator configured and running
- **THEN** the envelope's `data.emulators` array contains one entry for AWS with `"running": false` and one entry for Snowflake with `"running": true` and its full detail fields
- **AND** the command does not exit with an error solely because the AWS emulator is not running

#### Scenario: start reports one entry per configured emulator
- **WHEN** `lstk start --json` is run with two emulator types configured
- **THEN** the envelope's `data.emulators` array contains one entry per configured emulator type

### Requirement: Snapshot commands report the documented payload shapes
`snapshot save`, `snapshot load`, `snapshot list`, `snapshot show`, and `snapshot remove` SHALL each report the payload shape documented in design.md's Command Catalog for their respective destination kind (`local`, `pod`, or `s3` where applicable).

#### Scenario: snapshot save reports destination kind
- **WHEN** `lstk snapshot save pod:my-baseline --json` is run successfully
- **THEN** the envelope's `data` includes `"kind": "pod"`, `"podName": "my-baseline"`, and the resulting `version`

#### Scenario: snapshot show mirrors existing metadata fields
- **WHEN** `lstk snapshot show pod:my-baseline --json` is run successfully
- **THEN** the envelope's `data` includes `name`, `version`, `services`, and a `resources` array with per-service resource counts

### Requirement: Confirmation-gated commands report CONFIRMATION_REQUIRED instead of a generic error
`volume clear`, `reset`, and `snapshot remove`, when run with `--json` outside an interactive terminal and without `--force`, SHALL reject with `error.code = "CONFIRMATION_REQUIRED"` rather than a generic or unclassified error, since this is the single most common actionable failure a script driving these commands will hit.

#### Scenario: volume clear without --force under --json
- **WHEN** `lstk volume clear --json` is run without `--force`
- **THEN** the envelope has `"status": "error"` and `"error": {"code": "CONFIRMATION_REQUIRED", ...}`
