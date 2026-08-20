# extension-bundling-distribution Specification

## Purpose

Automate shipping LocalStack's own bundled extensions (for example a closed-source `lstk-deploy`) so they are available immediately after a standard install, carry their help descriptions, and stay version-matched with the `lstk` binary across updates. This builds on the bundled-directory *resolution* delivered by the `extension-bundling` capability (which lets lstk run a bundled extension that is present); here we cover how bundled extensions get *there*, how they are updated, and how existing installs keep updating across the transition.

## ADDED Requirements

### Requirement: Bundled extensions are available after a standard install

A set of extensions MAY be designated as bundled and SHALL be installed alongside `lstk` by the same single installation command across supported distribution channels (binary archive, Homebrew cask, npm), placed in the bundled-extensions directory — the directory of the symlink-resolved lstk executable — and resolvable immediately as `lstk <name>` with no separate install step and no `PATH` change by the user. Concretely per channel: siblings of `lstk` at the binary-archive root; the Caskroom staged directory for Homebrew (the cask stages the whole archive; bundled extensions are not symlinked into `bin`); the platform-specific package directory (`@localstack/lstk-<os>-<arch>`) for npm, where the launcher-executed binary lives. The closed-source bundled binaries SHALL be built privately and pulled into the release build context without exposing source in the public repository.

#### Scenario: Bundled extension available immediately

- **WHEN** a user installs lstk via the standard installation command for any supported channel and `lstk-deploy` is bundled
- **THEN** `lstk deploy` resolves to the bundled extension with no extra install step

#### Scenario: Bundled extension found without PATH changes

- **WHEN** a user extracts the binary archive and places only `lstk` on `PATH`
- **THEN** a bundled `lstk-deploy` sibling is still resolved by `lstk deploy` because lstk searches the directory alongside its executable

#### Scenario: npm install places the set where the real binary lives

- **WHEN** a user runs `npm install -g @localstack/lstk`
- **THEN** the bundled extensions and descriptions file are present in the platform package directory containing the Go binary the launcher executes
- **AND** the wrapper package, its `bin` entry, and the launcher behavior are unchanged

#### Scenario: Bundled extension runs on macOS without a Gatekeeper block

- **WHEN** a user installs via the Homebrew cask on macOS and runs a bundled extension for the first time
- **THEN** the extension executes without a Gatekeeper/quarantine prompt, because the cask's post-install hook de-quarantines the whole staged directory, not only the `lstk` binary

### Requirement: Bundled extensions update as one set with lstk

Updating lstk SHALL replace the lstk executable, its bundled extensions, and the descriptions file as one version-matched set on every install method. On Homebrew and npm this is inherited from whole-package replacement by the package manager. On the self-managed binary channel, `internal/update` SHALL stage every member of the new set next to its destination and rename each into place (the lstk binary last), guaranteeing that: no partially-written file is ever visible under a final name; a failure before commit leaves the installation untouched; and an interrupted commit is fully repaired by re-running `lstk update`. The binary channel is additive-only: it SHALL NOT delete an `lstk-*` sibling that is absent from the new archive (ownership of such files cannot be established; see design).

#### Scenario: Bundled extensions updated with lstk

- **WHEN** lstk is updated to a new version that ships a newer bundled `lstk-deploy`
- **THEN** the bundled `lstk-deploy` is replaced with the matching version as part of the same update on every install method

#### Scenario: Interrupted binary-channel update is safe and recoverable

- **WHEN** a binary-channel update is interrupted at any point
- **THEN** every file visible under a final name is complete (never truncated or partially written)
- **AND** re-running `lstk update` completes the replacement of the whole set

#### Scenario: Renamed or dropped extension

- **WHEN** lstk is updated to a version whose bundle renames or drops an extension
- **THEN** on Homebrew and npm the old binary is gone (whole-package replacement)
- **AND** on the binary channel the old binary MAY remain (additive-only) but appears name-only in help, because the replaced descriptions file no longer describes it

### Requirement: Existing installs keep updating across the transition

Introducing bundled-extension distribution SHALL NOT break `lstk update` for any existing install, in either direction. The update entry points per install method (`brew upgrade` for Homebrew, `npm install -g` for npm, archive download-verify-replace for binary) are unchanged. The conventions in-the-field updaters depend on SHALL be preserved: the archive name template and `checksums.txt` manifest, the lstk binary's name and archive-root location, the npm package names / wrapper `bin` / launcher contract, and the cask name, tap, and `binary "lstk"` stanza.

Bundled extensions are payload rather than a precondition in one sense only: **an archive that carries none is a valid archive**. A release shipping no bundled extensions — a pre-bundling release, or a rollback to one — SHALL update successfully as a set of size one. When an archive does carry bundled extensions they are **not optional**: the update SHALL install the complete set or fail, and a partial set SHALL NOT be reported as a successful update.

#### Scenario: Pre-bundling lstk updates into the first bundling release (binary)

- **WHEN** a user on a pre-bundling lstk runs `lstk update` and the latest release bundles extensions
- **THEN** the update succeeds using the in-the-field updater (which replaces only the lstk binary and ignores the archive's extra members)
- **AND** the install is left with an incomplete set, since that updater predates bundling and cannot be made to fail
- **AND** the incomplete set is repaired by the next `lstk update`, which SHALL NOT wait for a newer release to become available

#### Scenario: Pre-bundling lstk updates via Homebrew or npm

- **WHEN** a user on a pre-bundling lstk installed via Homebrew or npm runs `lstk update`
- **THEN** the package manager replaces the whole package and the bundled extensions are present immediately after that single update

#### Scenario: Rollback to an extension-free release

- **WHEN** the new set-wise updater applies an archive that carries no bundled extensions
- **THEN** the update succeeds, replacing only the lstk binary (a set of size one)

#### Scenario: Bundled extensions fail to install

- **WHEN** the set-wise updater applies an archive that carries bundled extensions and any member fails to stage or commit
- **THEN** the update fails with an error naming the member that failed
- **AND** the installation is left on its previous version rather than reporting success with an incomplete set

#### Scenario: Incomplete bundled set is repaired when lstk is already current

- **WHEN** `lstk update` runs on an install whose lstk binary is already the latest version but whose bundled set is incomplete
- **THEN** the update SHALL NOT report "already up to date"
- **AND** it installs the missing members of the set
- **AND** on the binary channel, previously installed bundled extensions remain in place and still run

### Requirement: Bundle provenance is resolved once, recorded, and verified

Each lstk release SHALL ship exactly one extensions bundle, identified by a version file committed to the lstk repository. That file SHALL default to `latest` — the newest published bundle — and SHALL accept an explicit release tag to lock a build to one bundle. The release process SHALL resolve the value to a concrete tag once per build and use that resolved tag for every subsequent step, and SHALL record it in the published release notes so the mapping from an lstk version to an extensions bundle survives independently of build-log retention. It SHALL download the resolved bundle's prebuilt binaries and descriptions file from the private extensions repository's release assets, SHALL verify every downloaded asset against the bundle's checksum manifest before staging (hard fail on a missing or mismatching manifest), and SHALL fail when a bundled extension lacks a binary for any lstk target platform not explicitly allow-listed as unsupported. The credential used is read-only and scoped to the private extensions repository.

#### Scenario: Checksum mismatch blocks the release

- **WHEN** a downloaded bundled binary does not match the bundle's checksum manifest
- **THEN** the release fails before any artifact is built

#### Scenario: Missing platform coverage blocks the release

- **WHEN** the resolved bundle has no `lstk-deploy` binary for a supported lstk platform that is not allow-listed as unsupported
- **THEN** the release fails at the pull step with an error naming the missing platform

#### Scenario: Re-running a published release reproduces the same bundle

- **WHEN** the release job is re-run for a tag that has already been published
- **THEN** it builds against the bundle recorded for that release rather than re-resolving `latest`
- **AND** the extension binaries it publishes are identical to those the original run published

#### Scenario: Bundle identity is recoverable from a released version

- **WHEN** someone needs to know which extensions bundle a given released lstk version shipped
- **THEN** the resolved bundle tag is available from that release's published notes

### Requirement: Hand-authored descriptions file, validated at release time

A static descriptions file that maps each bundled extension's command name to a one-line description SHALL ship with the distribution where lstk reads it (alongside the bundled extensions). The file is hand-authored and owned by LocalStack's private extensions repository — the same source of truth that produces the bundled binaries — and lstk's open-source repository SHALL assume it exists rather than generate it. The file SHALL cover only bundled, LocalStack-controlled extensions; it is not a per-extension manifest authored by third parties. The release process SHALL validate the file against the staged binaries so a name described in the file but with no shipped binary is a release-blocking error; a shipped binary with no description is permitted (it degrades to name-only help). lstk reads this file for help rendering (see the extension-framework capability) and never executes an extension to obtain a description.

#### Scenario: Descriptions file ships with the bundled set

- **WHEN** a release bundles `lstk-deploy`
- **THEN** the hand-authored descriptions file contains an entry mapping `deploy` to its one-line description
- **AND** that file is shipped where lstk resolves bundled extensions

#### Scenario: Release validation rejects a description with no shipped binary

- **WHEN** the descriptions file describes `deploy` but no `lstk-deploy` binary is present in the staged bundled set
- **THEN** the release-time validation fails, so lstk never ships a help entry for an extension that was not bundled

#### Scenario: Descriptions update atomically with the bundled set

- **WHEN** lstk is updated to a version that bundles a renamed or re-described extension
- **THEN** the descriptions file is updated as part of the same update
- **AND** lstk never shows a description that disagrees with the bundled binaries
