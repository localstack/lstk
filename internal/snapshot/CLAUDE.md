# Snapshots Reference (internal/snapshot)

Detail moved out of the root CLAUDE.md; see the root file for the command list.

## REF parsing

A REF is parsed by helpers in `internal/snapshot/destination.go`:
- **local file** — absolute/relative path; the `.snapshot` extension is forced (any other extension is replaced). On load, `.zip` files saved by older lstk versions are still accepted.
- **cloud snapshot** — `pod:` prefix (e.g. `pod:my-baseline`), stored on the LocalStack platform. Requires auth (`LOCALSTACK_AUTH_TOKEN` or `lstk login`).
- **S3 remote** — `s3://bucket/prefix` (parsed to `KindS3`). The CLI never touches S3; the emulator performs the transfer.

`ParseDestination` (save), `ParseSource` (load), `ParseRemovable` (remove), and `ParseShowable` (show) share pod-name validation; `ParseRemovable` and `ParseShowable` reject local paths (via the shared `parseCloudOnly` helper) so those cloud-only commands never touch local files.

## S3 remotes (save/load/list only)

`lstk snapshot save <pod-name> s3://bucket/prefix`, `load <pod-name> s3://bucket/prefix`, and `list s3://bucket/prefix` store snapshots in the user's own S3 bucket. The pod name (the snapshot's identity within the bucket) is a positional separate from the `s3://` location — required for load, auto-generated for save when omitted, unused for list. Credentials follow AWS CLI precedence (`resolveS3Credentials` in `cmd/snapshot.go`): `--profile <name>` wins, else static `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` (optional `AWS_SESSION_TOKEN`), else the profile named by `AWS_PROFILE` (read via `internal/awsconfig.ReadProfileCredentials`/`CredentialsFromEnv`); only static credentials are supported (no SSO/assume-role/credential_process — those resolve only via the AWS SDK chain, not our ini parser); **never put credentials in the URL** (rejected by `parseS3`). The emulator-side S3 remote requires a remote to be registered by name first, so the CLI transparently upserts a deterministic remote (`POST /_localstack/pods/remotes/<name>`, name derived in `internal/snapshot/remote.go`) with a placeholder-templated URL, then passes the real credentials as ephemeral `remote_params` on each op — secrets stay out of the registered URL and out of any persisted state. `list s3://…` queries the emulator (`GET /_localstack/pods` with a remote body), not the platform API, so it **requires a running emulator** (unlike `snapshot list`). Before save/load/list, lstk runs a pre-flight bucket-existence check (`ensureBucketExists` → `RemoteClient.S3BucketExists`, an unsigned S3 `HEAD`: 404 ⇒ missing) and errors out rather than letting the emulator auto-create a bucket on a typo; local-testing endpoints (IP / `host.docker.internal`) are skipped, and a check that can't run degrades to a warning. Domain logic + client interface live in `internal/snapshot/remote.go`; the emulator HTTP impl is `internal/emulator/aws/remote.go`; command wiring/arg classification (`classifyRemoteArgs`, `resolveS3Credentials`) is in `cmd/snapshot.go`. ORAS and other remotes, and `remove`/`show`/versions on S3, are not yet supported.

## Auto-load on start

A `[[containers]]` block (AWS only) can set `snapshot = "pod:my-baseline"` (any load REF) to auto-load that snapshot after the emulator starts. The loader runs only when the emulator is freshly started this run (skipped when already running), mirroring v1's `AUTO_LOAD_POD`. `lstk start --snapshot REF` overrides the configured REF for one run; `lstk start --no-snapshot` skips it. Resolution lives in `resolveStartSnapshotRef`/`newSnapshotAutoLoader` in `cmd/snapshot.go`; the loader is threaded into the non-interactive start in `cmd/root.go` and into the TUI via `ui.RunOptions.PostStart`. `snapshot save` never writes back into config — the `snapshot` field is manual.
