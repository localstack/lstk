## ADDED Requirements

### Requirement: A persistent setting controls the automatic update check
lstk SHALL read an update-check mode from the `[cli] update_check` key in `config.toml` and from the `LSTK_UPDATE_CHECK` environment variable, accepting exactly the values `prompt`, `notify`, and `off`. The mode SHALL govern the automatic update check performed on the start path, and SHALL NOT govern an explicit `lstk update` invocation.

Resolution order, first match wins: `LSTK_UPDATE_CHECK`, then `[cli] update_check`, then `notify` when the install is detected as externally managed (see the `external-install-detection` capability), then `prompt`.

Behavior per mode:

- `prompt` — lstk checks for a newer version and, on an interactive start, presents the blocking update choice.
- `notify` — lstk checks for a newer version and emits a single non-blocking note naming the current and latest version and how to update. No prompt is presented and nothing waits for input. When the install is externally managed, the note SHALL name that manager rather than advising `lstk update`, which refuses on such an install — regardless of how the mode was reached, since the advice is equally wrong either way.

An externally-managed install SHALL NOT be prompted even under `prompt`: applying the update is refused on such an install, and offering an action that cannot be carried out is worse than not offering it. `prompt` therefore behaves as `notify` there.

- `off` — lstk performs no version check at all: no network request is made and no update-related output is emitted.

An unrecognized value SHALL be rejected as a configuration error naming the key and the accepted values, rather than silently falling back to a default.

#### Scenario: Default behavior is unchanged
- **WHEN** neither `LSTK_UPDATE_CHECK` nor `[cli] update_check` is set and the install is not detected as externally managed
- **THEN** the resolved mode is `prompt`
- **AND** an interactive `lstk start` with a newer version available presents the blocking update choice, as before this change

#### Scenario: notify never blocks
- **GIVEN** `[cli] update_check = "notify"`
- **WHEN** `lstk start` runs interactively and a newer version is available
- **THEN** a single note naming both versions is emitted
- **AND** no prompt is presented and the start proceeds without waiting for input

#### Scenario: off makes no network request
- **GIVEN** `[cli] update_check = "off"`
- **WHEN** `lstk start` runs
- **THEN** no request is made to the release API
- **AND** no update-related output is emitted in either interactive or non-interactive mode

#### Scenario: Environment variable overrides config
- **GIVEN** `[cli] update_check = "off"` in `config.toml`
- **WHEN** `lstk start` runs with `LSTK_UPDATE_CHECK=prompt`
- **THEN** the resolved mode is `prompt`

#### Scenario: Invalid value is rejected
- **WHEN** `lstk start` runs with `[cli] update_check = "quiet"`
- **THEN** lstk exits non-zero with a configuration error naming `update_check` and the values `prompt`, `notify`, `off`
- **AND** the emulator is not started

#### Scenario: The setting does not disable the explicit update command
- **GIVEN** `[cli] update_check = "off"`
- **WHEN** `lstk update --check` is run
- **THEN** the version check is performed and its result reported as usual

### Requirement: The update prompt offers exactly three choices
The blocking update prompt SHALL offer "Update now", "Remind me next time", and "Never ask again" — and no per-version "Skip this version" option. A skipped version bought a few days of quiet against a weekly release cadence (the complaint behind DEVX-1029) while adding a third flavour of "no" to the prompt and the only piece of per-version persisted state; the permanent opt-out serves the same need without either cost. `cli.update_skipped_version` is removed with it.

"Never ask again" SHALL be offered **only when the setting can actually be persisted** — i.e. when a config file exists. On a first run config.toml has not been created yet (the emulator picker creates it later), and an option whose effect would be silently dropped SHALL NOT be offered.

Selecting it SHALL persist `cli.update_check = "notify"` to the config file in use, preserving the file's existing comments and formatting, and SHALL NOT apply an update.

A failure to persist SHALL be surfaced as a warning and SHALL NOT be reported as success.

#### Scenario: The opt-out is not offered when it cannot be persisted
- **GIVEN** no config file exists yet (a first run)
- **WHEN** the update prompt is presented
- **THEN** it offers only "Update now" and "Remind me next time"
- **AND** no option claims a preference was saved

#### Scenario: Never ask again persists the setting
- **GIVEN** an interactive `lstk start` with a newer version available and the mode resolved to `prompt`
- **WHEN** the user selects "Never ask again"
- **THEN** `cli.update_check` is written as `notify` to the config file reported by `lstk config path`
- **AND** no update is applied
- **AND** a subsequent `lstk start` with a newer version available emits a note instead of a prompt

#### Scenario: Persisting the opt-out fails
- **GIVEN** the config file cannot be written
- **WHEN** the user selects "Never ask again"
- **THEN** a warning is emitted naming the failure
- **AND** the command continues and exits as it otherwise would
