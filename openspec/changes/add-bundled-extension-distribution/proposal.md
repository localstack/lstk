## Why

The `add-extension-mechanism` change ships the extension *mechanism* — lstk resolves and runs `lstk-<name>` executables (PATH and a bundled directory next to the binary) and conveys runtime context to them. It deliberately stops short of *distributing* LocalStack's own bundled extensions: the first release is a test bed where a bundled `lstk-<name>` is validated by manual placement. This change closes that loop — it automates packaging LocalStack's (possibly closed-source) bundled extensions into the install artifacts, ships their help descriptions, and keeps the `lstk`/`lstk-*` set version-matched across updates — so bundled extensions like `lstk-deploy` are available immediately after a standard install with no manual step.

## What Changes

- **Package bundled extensions into every install channel** (binary archive, Homebrew, npm) so they land in the directory lstk resolves, with no `PATH` change required by the user.
- **Pull the prebuilt closed-source bundled binaries from private CI** into the release build context, version-pinned to the lstk release, without exposing source in the public repository.
- **Ship a hand-authored descriptions file** (`lstk-extensions.toml`) alongside the bundled binaries, owned by LocalStack's private extensions repository, and **validate it at release time** against the staged binaries so a described-but-missing extension is a release-blocking error.
- **Update the `lstk`/`lstk-*` set atomically** in `internal/update`, so a running `lstk` and its bundled extensions are never left at mismatched versions across any install method.

## Capabilities

### New Capabilities

- `extension-bundling-distribution`: Automated distribution and version-matched co-update of LocalStack's bundled extensions — cross-channel packaging (binary archive, Homebrew, npm), the private-CI binary pull, the release-shipped + release-validated descriptions file, and atomic updates of the `lstk`/`lstk-*` set via `internal/update`. Builds on the bundled-directory *resolution* delivered by `extension-bundling` in `add-extension-mechanism`.

### Modified Capabilities

<!-- Extends extension-bundling (resolution) with distribution/update; no existing requirement is changed. -->

## Impact

- **Touched code**: `internal/update` (atomic replacement of the `lstk`/`lstk-*` family + descriptions file), `.goreleaser.yaml` (archive/cask/npm payload inclusion), the public release workflow (private-CI pull, version pinning), and `scripts/check-descriptions.sh` (release-time validation).
- **Packaging/release**: binary archive, Homebrew formula/cask, and npm package lay out bundled extensions where lstk resolves them; the release workflow pulls prebuilt closed-source bundled binaries from private CI, authenticated with a repository/organization secret.
- **Docs**: re-introduce `docs/extensions-bundling.md` (on-disk layout per channel, the release pipeline, atomic update, the descriptions file and its validation).
- **External dependencies/services**: a private artifact location for the prebuilt bundled binaries and a release-time credential to pull them.

## Deferred (future work)

- User-facing `lstk extension` management commands (`list`/`info`/`install`/`remove`) and a user-mutable managed extensions directory.
- Internet-download of third-party extensions and any associated allow-listing / signature verification.
