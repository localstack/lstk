# extension-bundling Specification

## Purpose

Allow lstk to resolve and run LocalStack's own extensions (for example a closed-source `lstk-deploy`) from a read-only directory next to the `lstk` binary, ahead of any same-named executable on `PATH`. This first-release capability covers only *resolving and running* a bundled extension that is present — enough to validate bundled extensions by placing them manually. Automated cross-channel packaging/distribution, the release-shipped descriptions file, and atomic version-matched co-update with `lstk` are out of scope here and are specified by the future `add-bundled-extension-distribution` change. User-driven install/remove of extensions and a user-mutable managed directory also remain deferred.

## Requirements

### Requirement: Bundled-extensions directory alongside the executable

lstk SHALL look for bundled extensions in a fixed directory derived from the location of its own executable, resolving symlinks so the directory is found even when `lstk` is invoked through a symlink or package shim (e.g. an npm `.bin` link). Bundled extension executables follow the same `lstk-<name>` naming convention as any other extension and SHALL NOT require a manifest. A bundled extension SHALL be resolved ahead of a same-named executable on `PATH`. This directory is owned by the lstk distribution and is read-only from the user's perspective: lstk SHALL NOT provide commands to add to or remove from it in this change.

#### Scenario: Bundled directory resolved through a symlink

- **WHEN** `lstk` is invoked via a symlink or package shim and an `lstk-deploy` is present in the bundled directory
- **THEN** lstk resolves its real executable location, finds the bundled-extensions directory, and can resolve `lstk deploy`

#### Scenario: Naming convention identifies bundled extensions

- **WHEN** the bundled-extensions directory contains an executable named `lstk-deploy`
- **THEN** lstk treats it as the `deploy` extension without reading any manifest

#### Scenario: Bundled extension wins over a PATH extension of the same name

- **WHEN** both the bundled directory and `PATH` contain an `lstk-deploy`
- **THEN** lstk runs the bundled one for `lstk deploy`

### Requirement: Bundled closed-source extensions still self-authorize

Bundling SHALL NOT change the authorization model: a bundled extension that gates on entitlement (for example a premium closed-source extension) SHALL perform its own authorization using the conveyed auth token exactly as a separately distributed extension would. lstk SHALL NOT treat a bundled extension as automatically entitled.

#### Scenario: Bundled premium extension enforces its own entitlement

- **WHEN** a bundled `lstk-deploy` requires entitlement and an unentitled user runs `lstk deploy`
- **THEN** lstk dispatches to the bundled extension and conveys the token
- **AND** the bundled extension performs its own authorization and refuses the unentitled user
