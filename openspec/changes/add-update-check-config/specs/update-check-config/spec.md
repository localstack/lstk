## ADDED Requirements

### Requirement: A persistent setting controls the automatic update check
lstk SHALL read a boolean from the `[cli] check_for_update_on_startup` key in `config.toml` and from the `LSTK_CHECK_FOR_UPDATE_ON_STARTUP` environment variable. It SHALL govern the automatic update check performed on the start path, and SHALL NOT govern an explicit `lstk update` invocation.

Resolution order, first match wins: the environment variable, then the config key, then a default of `true`.

Behavior:

- **enabled** (default) — lstk checks for a newer version. On an interactive start of a self-managed install it presents the blocking update choice; on an externally-managed install, or when the call site cannot prompt, it emits a single non-blocking note instead.
- **disabled** — lstk performs no version check at all: no network request is made and no update-related output is emitted.

It is a boolean rather than a three-mode enum because install detection already decides between prompting and a note; the only choice left to the user is whether to check at all.

A value that is not a boolean SHALL be rejected as a configuration error naming the key and the accepted values, rather than silently falling back to a default. An unset key SHALL stay distinguishable from an explicit `false`, so the default can apply.

#### Scenario: Default behavior is unchanged
- **WHEN** neither the environment variable nor the config key is set
- **THEN** the check is enabled
- **AND** an interactive `lstk start` on a self-managed install with a newer version available presents the blocking update choice, as before this change

#### Scenario: An externally-managed install is never prompted
- **GIVEN** the check is enabled
- **WHEN** `lstk start` runs interactively on an install recognized as externally managed and a newer version is available
- **THEN** a single note naming the managing tool is emitted
- **AND** no prompt is presented and the start proceeds without waiting for input

#### Scenario: A non-interactive start emits a note
- **GIVEN** the check is enabled
- **WHEN** `lstk start --non-interactive` runs and a newer version is available
- **THEN** a single note naming both versions is emitted and nothing waits for input

#### Scenario: Disabled makes no network request
- **GIVEN** `[cli] check_for_update_on_startup = false`
- **WHEN** `lstk start` runs
- **THEN** no request is made to the release API
- **AND** no update-related output is emitted in either interactive or non-interactive mode

#### Scenario: Environment variable overrides config
- **GIVEN** `[cli] check_for_update_on_startup = false` in `config.toml`
- **WHEN** `lstk start` runs with `LSTK_CHECK_FOR_UPDATE_ON_STARTUP=true`
- **THEN** the check is enabled

#### Scenario: A non-boolean value is rejected
- **WHEN** `lstk start` runs with `[cli] check_for_update_on_startup = "quiet"`
- **THEN** lstk exits non-zero with a configuration error naming `check_for_update_on_startup` and the accepted values
- **AND** the emulator is not started

#### Scenario: The setting does not disable the explicit update command
- **GIVEN** `[cli] check_for_update_on_startup = false`
- **WHEN** `lstk update --check` is run
- **THEN** the version check is performed and its result reported as usual

### Requirement: The update prompt offers exactly three choices
The blocking update prompt SHALL offer "Update now", "Remind me next time", and "Never check again" — and no per-version "Skip this version" option. A skipped version bought a few days of quiet against a weekly release cadence (the complaint behind DEVX-1029) while adding a third flavour of "no" to the prompt and the only piece of per-version persisted state; the permanent opt-out serves the same need without either cost. `cli.update_skipped_version` is removed with it.

"Never check again" SHALL be offered **only when the setting can actually be persisted** — i.e. when a config file exists. On a first run config.toml has not been created yet (the emulator picker creates it), and an option whose effect would be silently dropped SHALL NOT be offered.

Selecting it SHALL persist `cli.check_for_update_on_startup = false` to the config file in use, preserving the file's existing comments and formatting, and SHALL NOT apply an update. A failure to persist SHALL be surfaced as a warning and SHALL NOT be reported as success.

#### Scenario: The opt-out is not offered when it cannot be persisted
- **GIVEN** no config file exists yet (a first run)
- **WHEN** the update prompt is presented
- **THEN** it offers only "Update now" and "Remind me next time"
- **AND** no option claims a preference was saved

#### Scenario: Never check again persists the setting
- **GIVEN** an interactive `lstk start` with a newer version available and a config file present
- **WHEN** the user selects "Never check again"
- **THEN** `cli.check_for_update_on_startup` is written as `false` to the config file reported by `lstk config path`
- **AND** no update is applied
- **AND** a subsequent `lstk start` performs no check at all

#### Scenario: Persisting the opt-out fails
- **GIVEN** the config file cannot be written
- **WHEN** the user selects "Never check again"
- **THEN** a warning is emitted naming the failure
- **AND** the command continues and exits as it otherwise would
