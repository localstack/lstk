## Context

`add-extension-mechanism` delivers the extension *mechanism* and bundled-directory *resolution*: lstk runs an `lstk-<name>` found next to its binary, ahead of `PATH`. It intentionally defers *distribution* so the first release can validate bundled extensions by manual placement. This change automates getting LocalStack's bundled extensions into the install artifacts and keeping them version-matched. The code for these decisions was prototyped during `add-extension-mechanism` and then removed from that change; this change re-introduces it.

## Decisions

### Decision 1: Atomic, version-matched update of the `lstk`/`lstk-*` set

`internal/update` treats `lstk` and its bundled `lstk-*` set (binaries + the descriptions file) as one unit. For the self-managed binary channel, the extractor stages every new `lstk`/`lstk-*` member next to its destination (`.lstk-new` siblings) and renames each into place, so an interrupted update never leaves `lstk` and a bundled extension at mismatched versions. For Homebrew and npm, the package manager replaces the whole package — and therefore the whole bundled set — atomically.

**Rationale**: a bundled extension and the `lstk` that conveys its contract are released together; a mismatched pair could violate the `LSTK_EXT_API_VERSION` contract. Staging-then-rename keeps the swap crash-safe within a directory.

### Decision 2: Hand-authored descriptions file, release-validated by a shell script

The descriptions file (`lstk-extensions.toml`) is hand-authored in LocalStack's private extensions repository — the same source of truth that builds the closed-source binaries — and shipped as-is. The open-source repo does not generate it. A release-time bash script, `scripts/check-descriptions.sh` (consistent with `scripts/test-integration.sh`), extracts the described command names (the bare left-hand identifiers of the flat `name = "…"` table — values are never parsed) and fails the release if any described name has no corresponding `lstk-<name>` binary in the staged dir. A staged binary with no description is allowed (help degrades to name-only).

**Validation targets a single, host-native staging dir** — descriptions are os/arch-independent, so the check runs once against one staging dir (the release host's own OS), where binaries are bare `lstk-<name>` with no `.exe`/PATHEXT ambiguity.

**Rationale**: validating (not generating) keeps one source of truth in the private repo while preserving the version-lock guarantee; key-only parsing keeps the shell check trivially correct.

**Alternatives considered**: a Go validator reusing the runtime `scanDir` + go-toml (rejected: extra build entrypoint and against the repo's "domain logic in Go, helpers in shell" grain for a small set-difference); generating the file from an in-repo manifest (rejected: duplicates a list the private repo already owns).

### Decision 3: Cross-channel packaging places bundled files where lstk resolves them

GoReleaser includes the staged bundled binaries and the descriptions file at each archive root (and in the Homebrew/npm payloads), siblings of `lstk`. The public release workflow pulls the prebuilt closed-source binaries for each `os/arch` from a private artifact location into a `bundled/<os>_<arch>/` staging dir, authenticated with a repository/organization secret; only binaries are pulled, never source. The GoReleaser inclusion is gated/commented until the private-CI pull is wired, so the credential-less open-source build never fails on an empty glob.

**Rationale**: lstk resolves the directory next to its symlink-resolved executable, so every channel must land bundled files there — sibling-at-archive-root for tarballs, libexec for Homebrew, package dir for npm.
