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

**Automated weekly release** (the default path): every Thursday, the workflow checks whether `main` has any commits since the last `v*.*.*` tag. If so, it inspects the labels on every PR merged since that tag, picks the highest release label found (`major` > `minor` > `patch`, defaulting to `patch`), runs full CI, then waits for a reviewer to approve the [Windows smoke test](#windows-smoke-test-required-before-every-release). Once approved, it creates and pushes the next version tag — which in turn triggers `LSTK CI`'s `release` job below. If there are no changes since the last tag, it skips the release entirely.

**Manual release**: run `Create Release Tag` from GitHub Actions (default ref: `main`), choosing a `patch` or `minor` bump, when you need to cut a release outside the weekly schedule. It waits for the same smoke-test approval before tagging.

Either path pushes a version tag (e.g. `v0.2.4`), which triggers `LSTK CI`, running the `release` job to publish the GitHub release with GoReleaser.

To validate release packaging locally without publishing:

```bash
goreleaser release --snapshot --clean
```

## Windows smoke test (required before every release)

GitHub-hosted Windows runners cannot run Linux containers, so CI never exercises `lstk` against a real Docker daemon on Windows. Both release workflows therefore stop before tagging: the tag job is bound to the `release` GitHub environment and waits until one of its required reviewers approves. The reviewers are configured in the repository's environment settings.

The run summary of the job before the gate (`Determine version bump` or `Resolve release ref`) shows the exact commit to test. The tag job later checks out that same commit, so `main` moving while the run waits does not change what gets released.

Before approving:

1. Build `lstk.exe` from the commit shown in the run summary, e.g. on the Windows machine:

   ```powershell
   git fetch origin; git checkout <sha>
   go build -o lstk.exe .
   ```

   Or cross-compile from macOS/Linux and copy the binary over: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o lstk.exe .`

2. On Windows 11 with Docker Desktop running (the setup most users have) and the AWS CLI installed, run:

   ```powershell
   .\lstk.exe start                                    # container comes up
   docker ps                                           # note the emulator container name
   docker inspect <container> --format '{{json .HostConfig.Binds}}'   # must include /var/run/docker.sock
   .\lstk.exe logs                                     # shows emulator output

   Set-Content handler.py 'def handler(event, context): return {"ok": True}'
   Compress-Archive handler.py function.zip -Force
   .\lstk.exe aws lambda create-function --function-name smoke `
     --runtime python3.12 --handler handler.handler `
     --role arn:aws:iam::000000000000:role/lambda-role --zip-file fileb://function.zip
   .\lstk.exe aws lambda wait function-active-v2 --function-name smoke
   .\lstk.exe aws lambda invoke --function-name smoke out.json; Get-Content out.json

   .\lstk.exe stop
   ```

   A successful invoke is the key check: Lambda spawns a sibling container through the mounted Docker socket, which is exactly what broke for Windows users before.

3. In the run, open **Review deployments**, tick `release`, and paste the OS, Docker Desktop version and result into the comment. Approve on success. Reject on failure, fix on `main`, and start a new release run.

Notes:

- Both release workflows share one concurrency group. Approve or reject a pending run before triggering another release.
- A run that is not approved within 30 days fails. Re-trigger it manually.
- Repository admins cannot bypass the gate; the environment is configured without admin bypass.

