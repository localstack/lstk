# Releasing `lstk`

## Choosing a semver label

Every PR must carry exactly one release label (enforced by `Require release label`). The automated release workflow uses these labels to compute the next version bump:

- `semver: patch` — anything that isn't a new feature: bug fixes, dependency bumps, internal refactors, docs, specs.
- `semver: minor` — new user-facing feature or command (e.g. a new subcommand, support for a new emulator).
- `semver: major` — reserved for breaking changes once lstk reaches 1.0; do not use before then.

## Release workflows

Release automation uses three workflows:

1. `Automated Weekly Release` (`.github/workflows/automated-release.yml`) — runs on a schedule (Thursdays) and can also be triggered manually.
2. `Create Release Tag` (`.github/workflows/create-release-tag.yml`) — manual-only.
3. `LSTK CI` (`.github/workflows/ci.yml`)

**Automated weekly release** (the default path): every Thursday, the workflow checks whether `main` has any commits since the last `v*.*.*` tag. If so, it inspects the labels on every PR merged since that tag, picks the highest release label found (`major` > `minor` > `patch`, defaulting to `patch`), runs full CI, then creates and pushes the next version tag — which in turn triggers `LSTK CI`'s `release` job below. If there are no changes since the last tag, it skips the release entirely.

**Manual release**: run `Create Release Tag` from GitHub Actions (default ref: `main`), choosing a `patch` or `minor` bump, when you need to cut a release outside the weekly schedule.

Either path pushes a version tag (e.g. `v0.2.4`), which triggers `LSTK CI`, running the `release` job to publish the GitHub release with GoReleaser.

To validate release packaging locally without publishing:

```bash
goreleaser release --snapshot --clean
```

