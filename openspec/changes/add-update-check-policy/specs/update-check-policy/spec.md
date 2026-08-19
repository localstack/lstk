## ADDED Requirements

### Requirement: The automatic update check has a three-valued, persistable policy

lstk SHALL support a `[cli] update_check` config value with exactly three values — `prompt`, `notify` and `off` — governing the automatic update check performed on the start path (bare `lstk` and `lstk start`).

- `prompt` SHALL show the interactive update prompt and wait for an answer. It is the default for an install lstk manages itself.
- `notify` SHALL emit a single non-blocking note reporting the available version and SHALL NOT wait for input.
- `off` SHALL perform no version check at all: no network request and no output.

The `LSTK_UPDATE_CHECK` environment variable SHALL accept the same three values and SHALL override the config file. Resolution precedence SHALL be `LSTK_UPDATE_CHECK`, then `[cli] update_check`, then the default implied by the install method.

Value matching SHALL ignore surrounding whitespace and letter case.

#### Scenario: Off suppresses both the notice and the request

- **WHEN** `[cli] update_check` is `"off"` and a newer version exists
- **THEN** no update notice or prompt appears
- **AND** lstk makes no request for release metadata

#### Scenario: Notify does not wait for input

- **WHEN** `[cli] update_check` is `"notify"`, the run is interactive, and a newer version exists
- **THEN** a one-line note reporting the current and latest version appears
- **AND** no prompt appears and the run proceeds without a keypress

#### Scenario: The environment variable overrides the config file

- **WHEN** `[cli] update_check` is `"off"` and `LSTK_UPDATE_CHECK` is `prompt`
- **THEN** the prompt appears
- **AND** when the two are reversed, no check is performed

### Requirement: An unparsable policy value is reported and ignored

An `update_check` value that is not one of the three SHALL NOT fail the command. lstk SHALL emit a warning naming the source it came from and the valid values, then continue resolving from the next source in precedence order, ending at the install-implied default.

#### Scenario: A typo warns and the check still runs

- **WHEN** `[cli] update_check` is `"yes"` and a newer version exists
- **THEN** a warning naming `update_check in [cli]` and listing `prompt, notify, off` appears
- **AND** the run continues, applying the default policy rather than skipping the check

### Requirement: A prompt is never shown where it cannot be answered

lstk SHALL treat a resolved `prompt` policy as `notify` when the run is not interactive, since only the interactive frontend can answer a user-input request.

#### Scenario: Non-interactive run notifies instead of prompting

- **WHEN** `[cli] update_check` is `"prompt"` and lstk runs non-interactively with a newer version available
- **THEN** the one-line note appears
- **AND** the run does not wait for input

### Requirement: Installs owned by another package manager are detected and left alone

lstk SHALL classify its install as externally managed when the resolved path of the running binary identifies a third-party package or version manager (`mise`, `asdf`, `Nix`, `Scoop`, `Chocolatey`), and SHALL record which manager it is.

Detection SHALL use the resolved path only. lstk's own install methods SHALL take precedence over any manager segment, so an npm- or Homebrew-installed lstk located inside a manager-owned tree remains an npm or Homebrew install and keeps updating through that mechanism.

A manager's directory name SHALL NOT be sufficient on its own where that name is also a plausible ordinary directory: detection SHALL additionally require one of that manager's own layout directories to follow it, so a checkout of the tool is not mistaken for an install by it.

An externally managed install SHALL default to the `notify` policy, and its note SHALL name the owning manager instead of directing the user to `lstk update`. An explicit `update_check` value SHALL override this default in both directions.

#### Scenario: A manager-owned install notifies and names the manager

- **WHEN** the running binary resolves under a `mise` install directory, nothing is configured, and a newer version exists
- **THEN** a one-line note appears naming mise and its own upgrade command
- **AND** no prompt appears

#### Scenario: An npm install under a manager-owned Node.js is still an npm install

- **WHEN** the running binary resolves to a `node_modules` path inside a mise- or asdf-managed Node.js installation
- **THEN** the install is classified as npm, not as externally managed
- **AND** `lstk update` updates it through npm

#### Scenario: A directory merely named after a manager is not a managed install

- **WHEN** the running binary resolves to a path containing a `mise`, `scoop` or `nix` segment that is not followed by one of that manager's layout directories
- **THEN** the install is classified as a plain binary
- **AND** `lstk update` replaces it in place as before

#### Scenario: An explicit policy overrides the externally managed default

- **WHEN** the install is externally managed and `update_check` is explicitly `"prompt"`
- **THEN** the prompt appears

### Requirement: lstk refuses to self-update an externally managed install

`lstk update` SHALL refuse to apply an update when another package manager owns the binary, exiting non-zero with the `UPDATE_EXTERNALLY_MANAGED` error code and an actionable message naming the manager and how to update through it. The refusal SHALL happen before any network request.

`lstk update --check` SHALL remain available on such an install, since it only reports. An `update_check` value of `off` SHALL NOT suppress an explicitly requested check: the setting governs the automatic check only.

#### Scenario: Applying an update is refused before any request

- **WHEN** `lstk update` runs from a manager-owned binary
- **THEN** it exits non-zero reporting `UPDATE_EXTERNALLY_MANAGED`, names the manager and its upgrade command
- **AND** no release metadata is requested and the binary is unchanged

#### Scenario: Checking still works, whatever the policy

- **WHEN** `lstk update --check` runs from a manager-owned binary, with `LSTK_UPDATE_CHECK` set to `off`
- **THEN** it exits zero and reports the available version

### Requirement: The prompt offers a durable opt-out

When lstk can persist to a config file, the update prompt SHALL offer a "Don't ask again" choice alongside updating, deferring and skipping. Choosing it SHALL persist `update_check = "notify"` — not `off` — preserving the file's existing comments and formatting, and SHALL confirm what was written, where, and how to reach the other policies.

The choice SHALL be omitted from the prompt when there is no config file to write to, rather than offered as a no-op.

#### Scenario: Choosing it takes effect on the next run

- **WHEN** the user chooses "Don't ask again" at the prompt
- **THEN** the config file records `update_check` as `notify`, with its existing comments and values intact
- **AND** a confirmation naming the file appears
- **AND** the next run emits the one-line note and does not prompt
