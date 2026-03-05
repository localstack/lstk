# Releasing `lstk`

Release automation uses two workflows:

1. `Create Release Tag` (`.github/workflows/create-release-tag.yml`)
2. `LSTK CI` (`.github/workflows/ci.yml`)

How it works:

1. Manually run `Create Release Tag` from GitHub Actions (default ref: `main`), choosing a `patch` or `minor` bump.
2. The workflow computes and pushes the next version tag (e.g. `v0.2.4`).
3. Pushing the tag triggers `LSTK CI`, which runs the `release` job and publishes the GitHub release with GoReleaser.

To validate release packaging locally without publishing:

```bash
goreleaser release --snapshot --clean
```

