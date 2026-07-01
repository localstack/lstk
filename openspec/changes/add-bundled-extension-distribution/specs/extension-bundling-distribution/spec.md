# extension-bundling-distribution Specification

## Purpose

Automate shipping LocalStack's own bundled extensions (for example a closed-source `lstk-deploy`) so they are available immediately after a standard install, carry their help descriptions, and stay version-matched with the `lstk` binary across updates. This builds on the bundled-directory *resolution* delivered by the `extension-bundling` capability (which lets lstk run a bundled extension that is present); here we cover how bundled extensions get *there* and stay correct.

## ADDED Requirements

### Requirement: Bundled extensions are available after a standard install

A set of extensions MAY be designated as bundled and SHALL be installed alongside `lstk` by the same single installation command across supported distribution channels (binary archive, Homebrew, npm), placed in the bundled-extensions directory, and resolvable immediately as `lstk <name>` with no separate install step. Packaging SHALL place bundled extensions where lstk resolves them without requiring the user to add them to `PATH`. The closed-source bundled binaries SHALL be built in private CI and pulled into the release build context version-pinned to the lstk release, without exposing source in the public repository.

#### Scenario: Bundled extension available immediately

- **WHEN** a user installs lstk via the standard installation command for any supported channel and `lstk-deploy` is bundled
- **THEN** `lstk deploy` resolves to the bundled extension with no extra install step

#### Scenario: Bundled extension found without PATH changes

- **WHEN** a user extracts the binary archive and places only `lstk` on `PATH`
- **THEN** a bundled `lstk-deploy` sibling is still resolved by `lstk deploy` because lstk searches the directory alongside its executable

### Requirement: Bundled extensions update atomically with lstk

Updating lstk SHALL update its bundled extensions to the matching version as a single, atomic set, so a running `lstk` and its bundled extensions are never left at mismatched versions. `internal/update` SHALL replace the lstk executable and its bundled extensions together regardless of the install method, or fail without partially updating.

#### Scenario: Bundled extensions updated with lstk

- **WHEN** lstk is updated to a new version that ships a newer bundled `lstk-deploy`
- **THEN** the bundled `lstk-deploy` is replaced with the matching version as part of the same update
- **AND** an interrupted update does not leave lstk and the bundled extension at mismatched versions

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
- **THEN** the descriptions file is updated as part of the same atomic update
- **AND** lstk never shows a description that disagrees with the bundled binaries
