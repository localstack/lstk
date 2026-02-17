# lstk
Localstack's new CLI (v2).

## Versioning
`lstk` uses calendar versioning in a SemVer-compatible format:

- `YYYY.M.patch`
- Example (current format): `2026.2.0`

Release tags are the source of truth for published versions.

## Version Output
The CLI exposes version info through:

- `lstk version`
- `lstk --version`

Output format:

- `lstk <version> (<commit>, <buildDate>)`

At local development time (without ldflags), defaults are:

- `version=dev`
- `commit=none`
- `buildDate=unknown`

## Releasing with GoReleaser
Release automation uses the CI workflow plus one helper workflow:

1. `Create Release Tag` (`.github/workflows/create-release-tag.yml`)
2. `LSTK CI` (`.github/workflows/ci.yml`)

How it works:

1. Manually run `Create Release Tag` from GitHub Actions (default ref: `main`).
2. The workflow computes and pushes the next CalVer tag for the current UTC month.
3. Pushing that tag triggers `LSTK CI`.
4. In `LSTK CI`, the `release` job runs only for tag refs and publishes the GitHub release with GoReleaser.

## Published Artifacts
Each release publishes binaries for:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`
- `windows/amd64`
- `windows/arm64`

Archive formats:

- `tar.gz` for Linux and macOS
- `zip` for Windows

Each release also includes `checksums.txt`.

## Local Dry Run
To validate release packaging locally without publishing:

```bash
goreleaser release --snapshot --clean
```
