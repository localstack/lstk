## 1. On-disk layout and packaging

- [ ] 1.1 Define the bundled-extensions on-disk layout next to the lstk binary (including the descriptions file) and how each channel populates it: binary archive (sibling files at the archive root), Homebrew (libexec, not symlinked to global bin), npm (package dir resolvable via the symlink-resolved exe path) — documented in `docs/extensions-bundling.md` (re-introduce this doc)
- [ ] 1.2 Wire GoReleaser archive/cask/npm payload inclusion of the staged `bundled/<os>_<arch>/lstk-*` binaries and `lstk-extensions.toml`; keep it gated/commented until the private-CI pull is wired so the credential-less open-source build does not fail on an empty glob

## 2. Private-CI binary pull

- [ ] 2.1 Wire the public release workflow to pull prebuilt closed-source bundled binaries (e.g. `lstk-deploy`) from private CI into a `bundled/<os>_<arch>/` staging dir, version-pinned to the lstk release, authenticated with a repository/organization secret, without exposing source

## 3. Descriptions file shipping + validation

- [ ] 3.1 Re-introduce `scripts/check-descriptions.sh` (bash, consistent with `scripts/test-integration.sh`): extract the described names from the hand-authored `lstk-extensions.toml` and fail the release if any has no corresponding `lstk-<name>` binary in the staged dir (a staged binary with no description is allowed; runs against one host-native staging dir since descriptions are os/arch-independent)
- [ ] 3.2 Wire the release process to pull the hand-authored descriptions file from the private extensions repo into the staging dir and run `scripts/check-descriptions.sh` against the staged binaries

## 4. Atomic version-matched update

- [ ] 4.1 Extend `internal/update` (`extract.go`) to replace lstk, its bundled `lstk-*` extensions, and the descriptions file as one atomic, version-matched set across all install methods (stage `.lstk-new` siblings, then rename); never leave them mismatched on an interrupted update — re-introduce `extract_test.go` coverage (`TestExtractAndReplaceUpdatesLstkSet`, `TestExtractAndReplaceAddsNewBundledExtension`)

## 5. Tests and docs

- [ ] 5.1 Integration test: bundled extension available immediately after a simulated standard install; bundled set updates atomically with lstk; descriptions file shipped where lstk reads it
- [ ] 5.2 Update `docs/extensions-bundling.md` and the CLAUDE.md Extensions section to document distribution + atomic update once enabled
