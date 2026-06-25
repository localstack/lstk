# extension-bundling Specification

## Purpose

Allow LocalStack to ship its own extensions (for example a closed-source `lstk-deploy`) by default alongside `lstk`, so they are available immediately after a standard install with no separate step, are kept in lockstep with the `lstk` version that ships them, and are resolved deterministically ahead of any same-named executable on `PATH`. This capability covers only static, read-only, ships-with-lstk extensions; user-driven install/remove of extensions and a user-mutable managed directory remain out of scope (deferred).

## ADDED Requirements

### Requirement: Bundled-extensions directory alongside the executable

lstk SHALL look for bundled extensions in a fixed directory derived from the location of its own executable, resolving symlinks so the directory is found even when `lstk` is invoked through a symlink or package shim (e.g. an npm `.bin` link). Bundled extension executables follow the same `lstk-<name>` naming convention as any other extension and SHALL NOT require a manifest. This directory is owned by the lstk distribution and is read-only from the user's perspective: lstk SHALL NOT provide commands to add to or remove from it in this change.

#### Scenario: Bundled directory resolved through a symlink

- **WHEN** `lstk` is invoked via a symlink or package shim and an `lstk-deploy` is bundled
- **THEN** lstk resolves its real executable location, finds the bundled-extensions directory, and can resolve `lstk deploy`

#### Scenario: Naming convention identifies bundled extensions

- **WHEN** the bundled-extensions directory contains an executable named `lstk-deploy`
- **THEN** lstk treats it as the `deploy` extension without reading any manifest

### Requirement: Bundled extensions are available after a standard install

A set of extensions MAY be designated as bundled and SHALL be installed alongside `lstk` by the same single installation command across supported distribution channels (binary archive, Homebrew, npm), placed in the bundled-extensions directory, and resolvable immediately as `lstk <name>` with no separate install step. Packaging SHALL place bundled extensions where lstk resolves them without requiring the user to add them to `PATH`.

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

### Requirement: Release-generated descriptions file for bundled extensions

The release process SHALL generate a static descriptions file that maps each bundled extension's command name to a one-line description, and SHALL ship it with the distribution where lstk reads it (alongside the bundled extensions). The file SHALL cover only bundled, LocalStack-controlled extensions; it is not a per-extension manifest authored by third parties. It SHALL be versioned and updated together with the bundled extension set, so descriptions never drift from the binaries that ship. lstk reads this file for help rendering (see the extension-framework capability) and never executes an extension to obtain a description.

#### Scenario: Descriptions file ships with the bundled set

- **WHEN** a release bundles `lstk-deploy`
- **THEN** the release process produces a descriptions file entry mapping `deploy` to its one-line description
- **AND** that file is shipped where lstk resolves bundled extensions

#### Scenario: Descriptions update atomically with the bundled set

- **WHEN** lstk is updated to a version that bundles a renamed or re-described extension
- **THEN** the descriptions file is updated as part of the same atomic update
- **AND** lstk never shows a description that disagrees with the bundled binaries

### Requirement: Bundled closed-source extensions still self-authorize

Bundling SHALL NOT change the authorization model: a bundled extension that gates on entitlement (for example a premium closed-source extension) SHALL perform its own authorization using the conveyed auth token exactly as a separately distributed extension would. lstk SHALL NOT treat a bundled extension as automatically entitled.

#### Scenario: Bundled premium extension enforces its own entitlement

- **WHEN** a bundled `lstk-deploy` requires entitlement and an unentitled user runs `lstk deploy`
- **THEN** lstk dispatches to the bundled extension and conveys the token
- **AND** the bundled extension performs its own authorization and refuses the unentitled user
