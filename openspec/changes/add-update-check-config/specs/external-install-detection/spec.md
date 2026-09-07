## ADDED Requirements

### Requirement: lstk recognizes an externally-managed install
lstk SHALL classify its own install as externally managed when the resolved path of the running executable (after symlink resolution) matches a known tool-manager or immutable-store marker, and SHALL record which manager was recognized. The recognized managers are nix, guix, mise, asdf, scoop, and chocolatey.

The existing Homebrew and npm classifications SHALL take precedence: a path matching an npm or Homebrew marker anywhere SHALL classify as npm or Homebrew respectively, even when a tool-manager marker also appears in the path. This is required because a Homebrew- or npm-installed lstk may live under a tool-manager-provisioned interpreter, and both of those install methods can still update themselves correctly.

#### Scenario: A mise install is recognized
- **WHEN** the resolved executable path is `/home/user/.local/share/mise/installs/github-localstack-lstk/latest/lstk`
- **THEN** the install is classified as externally managed by mise

#### Scenario: A nix install is recognized
- **WHEN** the resolved executable path is under `/nix/store/`
- **THEN** the install is classified as externally managed by nix

#### Scenario: npm under a tool-managed interpreter stays npm
- **WHEN** the resolved executable path is `/home/user/.local/share/mise/installs/node/24.8.0/lib/node_modules/@localstack/lstk_linux_amd64/lstk`
- **THEN** the install is classified as npm, not as externally managed

#### Scenario: An ordinary install is not externally managed
- **WHEN** the resolved executable path is `/usr/local/bin/lstk` or `/home/user/bin/lstk`
- **THEN** the install is classified as a standalone binary

### Requirement: Every path that replaces the binary is guarded
The refusal SHALL be enforced at the point the binary is actually replaced, not only at the `lstk update` entry point. In particular the start-path update prompt's "Update now" SHALL be subject to it, so that no combination of settings can reach an in-place replacement of an externally-managed install without `--force`.

#### Scenario: The start-path prompt cannot clobber an externally-managed install
- **GIVEN** lstk is running from a path recognized as managed by mise
- **AND** `[cli] update_check = "prompt"` is set explicitly
- **WHEN** `lstk start` runs interactively with a newer version available
- **THEN** no update prompt is presented
- **AND** a note naming mise is emitted instead

### Requirement: An externally-managed install is not updated in place
`lstk update` SHALL determine whether the install is externally managed before performing any version check or download, and SHALL refuse to update when it is — reporting the recognized manager and the resolved executable path, and stating that the update must go through that manager. The refusal SHALL exit non-zero and, under `--json`, SHALL carry the error code `UPDATE_EXTERNALLY_MANAGED`.

`lstk update --check` SHALL be exempt from the refusal: it reports whether a newer version exists and writes nothing, which is useful however lstk was installed.

lstk SHALL also refuse an in-place binary replacement when the resolved executable's directory cannot be written to, under the same error code, reporting the directory instead of a manager. Homebrew and npm installs SHALL never be refused on this basis, since they delegate to `brew upgrade` and `npm install -g` rather than writing the file themselves. A probe that cannot determine writability SHALL NOT refuse.

`lstk update --force` SHALL bypass the refusal and update as if the install were a standalone binary, so that a misidentified install is never left without a path forward.

#### Scenario: Update refuses on an externally-managed install
- **GIVEN** lstk is running from a path recognized as managed by mise
- **WHEN** `lstk update` is run
- **THEN** lstk exits non-zero naming mise and the resolved executable path
- **AND** no release archive is downloaded and the executable is not replaced

#### Scenario: The refusal is machine-readable
- **GIVEN** lstk is running from a path recognized as managed by nix
- **WHEN** `lstk update --json` is run
- **THEN** the envelope reports `"status": "error"` with `"error": {"code": "UPDATE_EXTERNALLY_MANAGED", ...}`

#### Scenario: --check is not refused
- **GIVEN** lstk is running from a path recognized as managed by mise
- **WHEN** `lstk update --check` is run
- **THEN** the version check is performed and its result reported, with no refusal

#### Scenario: A read-only install directory is refused
- **GIVEN** lstk is running as a standalone binary from a directory it cannot write to
- **WHEN** `lstk update` is run
- **THEN** lstk exits non-zero naming that directory
- **AND** no release archive is downloaded

#### Scenario: --force overrides the refusal
- **GIVEN** lstk is running from a path recognized as managed by mise
- **WHEN** `lstk update --force` is run
- **THEN** the version check and update proceed as they would for a standalone binary

### Requirement: Detection does not run on invocations that do not check for updates
Install-method detection SHALL NOT be performed on any code path that does not otherwise reach the automatic update check or the update command.

Within the **automatic start-path check** it SHALL NOT run when the resolved mode is `off`, or before the version check has reported that a newer version is available. This scoping is deliberate: `lstk update` detects unconditionally and up front, because its refusal must precede the version check and the download (see the requirement below), and that is a different code path with a different cost profile.

It SHALL run once an update *is* known to exist, whatever the mode (other than `off`) and whether or not the call site can prompt: its answer determines the note's wording as well as the prompt/note decision, so skipping it for an explicitly-set mode produced a note advising `lstk update` on an install where that command refuses.

#### Scenario: off never reaches detection
- **GIVEN** `[cli] update_check = "off"`
- **WHEN** `lstk start` runs
- **THEN** no version check is performed and install-method detection is not consulted

#### Scenario: Other commands never detect
- **WHEN** any command other than `lstk`, `lstk start`, or `lstk update` is run
- **THEN** install-method detection is not performed
