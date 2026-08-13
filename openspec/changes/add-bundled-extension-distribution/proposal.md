## Why

The `add-extension-mechanism` change ships the extension *mechanism* — lstk resolves and runs `lstk-<name>` executables (PATH and a bundled directory next to the binary) and conveys runtime context to them. It deliberately stops short of *distributing* LocalStack's own bundled extensions: the first release is a test bed where a bundled `lstk-<name>` is validated by manual placement. This change closes that loop — it automates packaging LocalStack's (possibly closed-source) bundled extensions into the install artifacts, ships their help descriptions, and keeps the `lstk`/`lstk-*` set version-matched across updates — so bundled extensions like `lstk-deploy` are available immediately after a standard install with no manual step.

The runtime half already exists and is not touched by this change: `extension.BundledDir` resolves the directory of the symlink-resolved lstk executable, `extension.LoadDescriptions` reads `lstk-extensions.toml` from it, and help rendering consumes both. This change is exclusively pipeline (getting the files into the artifacts) and update (keeping them version-matched afterwards).

## What Changes

- **Package bundled extensions into every install channel** so they land in the directory lstk resolves, with no `PATH` change required by the user:
  - binary archive: `lstk-*` binaries and `lstk-extensions.toml` as siblings of `lstk` at the archive root;
  - Homebrew: automatic via the cask's Caskroom staging of the whole archive (lstk ships as a **cask**, not a formula — no libexec involved); the cask's post-install quarantine hook is widened from the single `lstk` binary to the whole staged directory;
  - npm: bundled files are copied into each **platform package** (`@localstack/lstk-<os>-<arch>`), where the real binary lives — not the wrapper package — via a post-processing step in the release job.
- **Pull the prebuilt closed-source bundled binaries from the private extensions repository's releases** into the release build context, **version-pinned via a pin file in this repo** (`bundled/extensions.version`), checksum-verified against the private release's manifest, authenticated with a dedicated read-only token, without exposing source in the public repository.
- **Ship the hand-authored descriptions file** (`lstk-extensions.toml`), owned by LocalStack's private extensions repository, and **validate it at release time** (`scripts/check-descriptions.sh`) so a described-but-missing extension is a release-blocking error.
- **Update the `lstk`/`lstk-*` set as one unit** in `internal/update` for the self-managed binary channel (stage `.lstk-new` siblings, then rename, lstk last); Homebrew and npm replace the whole package — and therefore the whole set — via their package managers.
- **Guarantee update continuity**: `lstk update` keeps working for every existing install across the transition — a pre-bundling lstk updates cleanly into the first bundling release on all three channels (Homebrew and npm especially, where the updater shells out to the package manager), and a bundling lstk updates cleanly from an archive that carries no extensions (rollback). An archive carrying no extensions is a valid archive and must not fail an update — but when an archive does carry them they are not optional: the update installs the complete set or fails, and never reports success with a partial one.

## Capabilities

### New Capabilities

- `extension-bundling-distribution`: Automated distribution and version-matched co-update of LocalStack's bundled extensions — cross-channel packaging (binary archive, Homebrew cask, npm platform packages), the pinned + verified private-release pull, the release-shipped + release-validated descriptions file, set-wise updates of `lstk`/`lstk-*` via `internal/update`, and update continuity across the transition. Builds on the bundled-directory *resolution* delivered by `extension-bundling` in `add-extension-mechanism`.

### Modified Capabilities

<!-- Extends extension-bundling (resolution) with distribution/update; no existing requirement is changed. -->

## Impact

- **Touched code**: `internal/update/extract.go` (+ re-introduced `extract_test.go`) — set-wise stage-then-commit replacement; `.goreleaser.yaml` — archive payload entries and the cask quarantine hook; `.github/workflows/ci.yml` release job — private pull step, descriptions validation step, npm platform-package copy step; new `scripts/fetch-bundled-extensions.sh` and `scripts/check-descriptions.sh`; new `bundled/extensions.version` pin file (+ `.gitignore` entries for the staging dirs).
- **Packaging/release**: the binary archives gain `lstk-*` + `lstk-extensions.toml` at the root; the Homebrew cask inherits them via archive staging (hook widened); the npm platform packages gain them via post-processing; the release workflow pulls the pinned private release's binaries with a repository/organization secret (dedicated read-only PAT).
- **Docs**: re-introduce `docs/extensions-bundling.md` (on-disk layout per channel, the release pipeline, pin-bump process, local snapshot builds, update semantics and guarantees, rollback); update the CLAUDE.md Extensions section.
- **External dependencies/services**: the private extensions repository publishing tagged releases (per-platform binaries, `lstk-extensions.toml`, `checksums.txt`) and a release-time read-only credential to download them.
- **Public visibility note**: once embedded in the public release archives, the bundled extension *binaries* are publicly downloadable; only their *source* stays private. Any gating of what an extension does must happen at runtime (e.g. auth via `LSTK_EXT_CONTEXT`), not at distribution.

## Deferred (future work)

- User-facing `lstk extension` management commands (`list`/`info`/`install`/`remove`) and a user-mutable managed extensions directory.
- Internet-download of third-party extensions and any associated allow-listing / signature verification.
- Deleting stale bundled binaries on the self-managed binary channel when a release renames or drops an extension (requires an ownership manifest; folds into the managed-extensions-directory work). This change is additive-only there; Homebrew and npm remove stale members naturally via whole-package replacement.
