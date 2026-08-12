## Context

`add-extension-mechanism` (PR #340) delivered the extension *mechanism* and everything lstk needs at runtime: bundled-directory resolution (`extension.BundledDir` — the directory of the symlink-resolved lstk executable, searched ahead of PATH), descriptions loading (`extension.LoadDescriptions` reads `lstk-extensions.toml` from that directory, degrading to an empty map on any failure), and help wiring (`cmd/extension.go`). It intentionally deferred *distribution* so the first release could validate bundled extensions by manual placement. This change automates getting LocalStack's bundled extensions into the install artifacts and keeping them version-matched with lstk.

The distribution code was prototyped during `add-extension-mechanism` and removed before the squash-merge; it is **not recoverable from git history** (the squashed commit contains only the runtime code and these openspec docs). Everything below is designed against the current tree.

Because `BundledDir` is "the directory containing the symlink-resolved lstk executable", the whole distribution problem reduces to one rule: **each channel must land the bundled files in the directory where the real lstk binary lives.** Every decision below follows from that rule plus the constraints of the individual channel.

## Decisions

### Decision 1: Channel placement — one staging dir feeds all three channels (corrects two assumptions from the original draft)

A single release-time staging tree is the source for every channel: `bundled/<os>_<arch>/lstk-<name>[.exe]` plus one os/arch-independent `bundled/lstk-extensions.toml`.

**Binary archive** — the staged binaries and the descriptions file are added at the archive root as siblings of `lstk`, via `archives.files` entries in `.goreleaser.yaml` with a templated source (`bundled/{{ .Os }}_{{ .Arch }}/lstk-*`), `strip_parent: true`, and `info: { mode: 0o755 }` so execute bits survive download and staging.

**Homebrew** — lstk ships as a **cask** (`homebrew_casks` in `.goreleaser.yaml`), not a formula; the original draft's "libexec" plan described a formula layout that does not exist here. A cask stages the whole release archive under the Caskroom and symlinks only the declared `binary "lstk"` into `bin`. Since lstk resolves its bundled dir through `EvalSymlinks`, the Caskroom staged directory — containing every archive-root sibling — **is** the bundled dir, with zero layout work. Two cask constraints:

- The post-install hook currently de-quarantines only `#{staged_path}/lstk`; it must cover the staged directory recursively (`xattr -dr com.apple.quarantine "#{staged_path}"`), otherwise Gatekeeper blocks the first run of every bundled extension on macOS (the binaries are not notarized; quarantine stripping is the standard cask workaround, and it must now cover the whole set).
- Bundled extensions must **not** gain `binary` stanzas: they stay un-symlinked and are resolved only through the bundled dir. GoReleaser's generated cask declares only `lstk`; keep it that way (release-candidate checklist item).

**npm** — the real binary lives in the platform-specific optional-dependency package (`@localstack/lstk-<os>-<arch>`), not the `@localstack/lstk` wrapper: `npm/launcher.js` execs it from there, so `os.Executable()` — and therefore the bundled dir — is the *platform package* directory. Bundled files must be copied into each platform package. `goreleaser-npm-publisher build` has no per-platform extra-files mechanism, so the release job post-processes its `dist/npm/lstk-<os>-<arch>/` output with a copy step before `npm publish` — the same seam already used to swap in the signal-forwarding launcher — mapping Node platform names to Go names (`win32`→`windows`, `x64`→`amd64`; `darwin`/`linux`/`arm64` map to themselves).

**Rationale**: one resolution rule at runtime, one staging tree at build time; no channel grows its own layout concept, and the archive (already SHA-256-verified by the self-updater and hash-pinned by the cask) carries the extensions under the existing integrity checks for free.

### Decision 2: Bundled binaries come from the private repo's releases, pinned by a file in this repo

The private extensions repository — the same source of truth that builds the closed-source binaries and hand-authors the descriptions file — publishes **tagged releases** whose assets are the per-platform binaries (`lstk-<name>_<os>_<arch>[.exe]`), `lstk-extensions.toml`, and a `checksums.txt` manifest covering them.

This repo carries a **pin file**, `bundled/extensions.version` — a single line naming the private release tag. Each lstk release therefore maps deterministically and reproducibly to one extensions bundle; bumping the pin is an ordinary reviewable PR (automatable later from the private repo's release workflow). `scripts/fetch-bundled-extensions.sh` reads the pin, downloads the assets (`gh release download`), **verifies each against the bundle's `checksums.txt`**, and stages them under `bundled/` with canonical names. It hard-fails when any lstk target platform has no matching asset (subject to an explicit not-supported allowlist), so platform gaps surface at pull time, not as an empty-glob failure inside GoReleaser.

The credential is a **dedicated fine-grained read-only PAT** (contents: read on the private repo only), stored as a repository/organization secret — not a reuse of the broader `PRO_ACCESS_TOKEN`. Least privilege, independent rotation.

**Alternatives considered**: GitHub Actions artifacts from the private repo (rejected: 90-day maximum retention breaks re-running a release build and any rebuild-from-tag; awkward cross-repo API); an S3 bucket or OCI registry (rejected for now: new infrastructure and a second credential lifecycle for no gain at this scale; revisit if the bundle outgrows release-asset limits). Storage lifecycle: the private release assets are the canonical, permanent store; the CI staging dir lives only for the job; the public release archives (and npm packages) are the permanent distribution copies — the extension *binaries* become publicly downloadable there by design, only *source* stays private.

### Decision 3: Hand-authored descriptions file, release-validated by a shell script

The descriptions file (`lstk-extensions.toml`) is hand-authored in the private extensions repository and shipped as-is; the open-source repo never generates it. A release-time bash script, `scripts/check-descriptions.sh` (consistent with `scripts/test-integration.sh`), extracts the described command names — the bare left-hand identifiers of the flat `name = "…"` table (`^[[:space:]]*([A-Za-z0-9][A-Za-z0-9_-]*)[[:space:]]*=`); values are never parsed — and fails the release if any described name has no corresponding executable `lstk-<name>` in the staged dir. A staged binary with no description warns but passes (help degrades to name-only, per the `extension-bundling` spec).

**Validation targets a single, host-native staging dir** — descriptions are os/arch-independent, so the check runs once against the release runner's own platform staging dir (`linux_amd64`), where binaries are bare `lstk-<name>` with no `.exe`/PATHEXT ambiguity.

**Rationale**: validating (not generating) keeps one source of truth in the private repo while preserving the version-lock guarantee; key-only parsing keeps the shell check trivially correct.

**Alternatives considered**: a Go validator reusing the runtime `scanDir` + go-toml (rejected: extra build entrypoint and against the repo's "domain logic in Go, helpers in shell" grain for a small set-difference); generating the file from an in-repo manifest (rejected: duplicates a list the private repo already owns).

### Decision 4: Set-wise update = stage-then-commit; additive-only on the binary channel

Only the self-managed binary channel needs Go work — `brew upgrade` and `npm install -g` replace the whole package directory, which replaces the whole set (including removals and renames) by construction.

`internal/update/extract.go`'s `extractAndReplace` generalizes from "find `lstk` in the temp dir, rename it over the executable" to a stage-then-commit **set** replacement:

1. **Discover** the set at the extracted archive root: the lstk binary, every executable `lstk-*`, and `lstk-extensions.toml`. An archive with no extensions yields a set of size one — today's behavior, byte for byte.
2. **Clean orphans**: remove any `*.lstk-new` siblings left by a previously crashed update.
3. **Stage**: copy each member into the destination dir (the running executable's directory) as a `<name>.lstk-new` sibling — same directory ⇒ same filesystem ⇒ each upcoming rename is atomic — and set 0755 on binaries. Any failure here removes the staged files and leaves the installation untouched.
4. **Commit**: rename each `.lstk-new` over its final name — extensions and the descriptions file first, `lstk` itself **last**, so the load-bearing swap is the final act and "update reported success" implies the whole set committed. Windows keeps the existing rename-running-exe-to-`.old` dance for `lstk.exe` only; extensions are not running during `lstk update` and rename directly.

**The honest guarantee** (this is what the spec promises — not "atomic across files", which POSIX cannot deliver): no partially-written file is ever visible under a final name; the mismatch window is a handful of renames; an interrupted update is healed by re-running `lstk update` (the flow is idempotent). Momentary version skew inside that window is benign by contract: `LSTK_EXT_API_VERSION` bumps only on breaking changes.

**Additive-only**: the binary-channel update replaces and adds members but never deletes an `lstk-*` sibling absent from the new archive. Deleting safely requires knowing lstk *owns* the file — users may place their own extensions next to the binary, and the descriptions file is not an ownership manifest (the spec deliberately permits undescribed bundled binaries). A renamed/dropped extension therefore leaves its old binary behind on this channel only: it keeps working at its old version and shows name-only help (the replaced descriptions file no longer describes it, so help never disagrees with the shipped set). Deletion is deferred to the managed-extensions-directory work. Corollary: if a user manually placed an `lstk-<name>` in the install dir and a release later bundles that same name, the update overwrites it — that directory is lstk's install dir; PATH is the supported home for user-installed extensions.

**Alternatives considered**: making release validation two-sided so the descriptions file becomes a complete ownership manifest enabling safe deletion (rejected here: contradicts the `extension-bundling` allowance for undescribed binaries; revisit with the managed dir); a separate shipped manifest file (rejected: a third shipped file to solve a rename edge case).

### Decision 5: Update continuity is a hard requirement — no existing install may be cut off from `lstk update`

The transition release (the first that ships bundled extensions) must be reachable by every in-the-field updater, and a later extension-free release must be reachable from a bundling one (rollback). Per channel:

- **Binary**: the *current* in-the-field `extractAndReplace` extracts the whole archive to a temp dir and touches only the `lstk` member; extra `lstk-*`/toml files are ignored. A pre-bundling lstk therefore updates into a bundling release cleanly (it just doesn't install the extensions — the user's next update, running the new code, does). Constraints this imposes: the lstk binary stays at the archive root under the same name, and the archive `name_template`/`checksums.txt` conventions are frozen (`buildAssetName` in `internal/update/github.go` reconstructs them on user machines).
- **npm**: the wrapper's `bin`, the launcher contract, package names, and the wrapper→platform-package `optionalDependencies` pinning are all untouched — the platform packages merely gain payload files the launcher never reads. A pre-bundling install's `npm install -g @localstack/lstk` (what `updateNPM` runs) replaces wrapper and platform package wholesale and picks up the extensions.
- **Homebrew**: `brew upgrade localstack/tap/lstk` (what `updateHomebrew` runs) installs the new version using the **new** cask definition — so the widened quarantine hook takes effect on the very release that first ships extensions; the old Caskroom version dir is removed wholesale. The cask's `binary "lstk"` stanza and tap location are untouched.
- **Both directions**: the new set-wise updater treats an archive without extensions as a set of size one, so downgrades/rollbacks to pre-bundling releases work. Bundled extensions are **payload, never a precondition**: no update path may fail because extensions are missing from an archive, a staging dir, or an install.

### Decision 6: Release gating and local builds

The `archives.files` entries land commented until the private pull is wired, then pull + payload are enabled **in one PR**: the PR-level `goreleaser check` job only validates config syntax (globs are not evaluated), but `goreleaser release/build` fails on a glob with zero matches, so the entries must never be live without the staging step that populates `bundled/`. After enabling, local snapshot builds require `scripts/fetch-bundled-extensions.sh` first; its `--stub` mode stages placeholder files for local, never-published builds so contributors without the private-repo credential can still run `goreleaser` locally. The staging tree is gitignored (only the pin file is tracked); it deliberately does not live under `dist/`, which `goreleaser --clean` wipes at startup.

The first bundling release ships with the smallest viable bundle (a single extension) and is verified against the release-candidate checklist in `docs/extensions-bundling.md` — fresh install and upgrade-from-previous on all three channels — before further extensions are added to the bundle.
