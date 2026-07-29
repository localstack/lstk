## 1. Flag plumbing

- [ ] 1.1 Add `--endpoint-url` as a root `PersistentFlags()` string in `cmd/root.go` (alongside `--config`).
- [ ] 1.2 Extend `cmd/proxy.go`'s `globalFlags`/`stripGlobalFlags` so `DisableFlagParsing: true` commands (`aws`, `terraform`, `cdk`, `sam`) recognize and strip `--endpoint-url` before forwarding remaining args.
- [ ] 1.3 Give `cmd/az.go` the same `--endpoint-url` recognition it already has for `--json`.

## 2. Endpoint resolution and type detection

- [ ] 2.1 Add a resolution helper (e.g. `endpoint.ResolveTarget(cmd, cfg)`) that applies the precedence flag > `LSTK_ENDPOINT_URL` > `AWS_ENDPOINT_URL` > Docker discovery uniformly for every in-scope command (`AWS_ENDPOINT_URL` is a full synonym for `LSTK_ENDPOINT_URL`, not restricted to AWS-specific commands), returning either a resolved external URL or a signal to fall back to existing Docker-based resolution.
- [ ] 2.2 Validate the resolved URL (via `net/url`) has a scheme and host; fail fast with an actionable error otherwise.
- [ ] 2.3 Implement an HTTP health/type probe: `GET {endpoint}/_localstack/health`, falling back to `GET {endpoint}/_localstack/info` when the health response lacks `version` (Azure's quirk); classify the response as `aws`/`azure`/`snowflake` or return "inconclusive". There is no override flag or config setting for the type — an inconclusive result is a hard failure (task 2.5), not a fallback to a manual value.
- [ ] 2.4 Confirm the AWS-vs-Snowflake `services`-map signature against real LocalStack AWS and Snowflake health payloads (see design.md Open Questions); adjust the classification logic accordingly. If the signature proves unreliable, the fix belongs in the emulator's health/info payload, not in an lstk-side override.
- [ ] 2.5 Produce a distinct "unreachable/not a LocalStack emulator" error type (no Docker/`lstk start` language) for probe failures, and a distinct "couldn't determine emulator type" error for inconclusive detection (no flag suggested); both reused by all call sites in section 3.
- [ ] 2.6 Add a helper for the rejection path used by `logs`/`stop`/`restart`/`volume` (section 3.8): an explicit `--endpoint-url` flag on the command line errors with an actionable "not supported here" message; an ambient `LSTK_ENDPOINT_URL`/`AWS_ENDPOINT_URL` is silently ignored. `snapshot show`/bare `list` need no equivalent helper — they simply never consult the resolved endpoint target, so an explicit flag is silently a no-op with zero code required (Cobra already accepts an unused persistent flag).

## 3. Command preflight changes

- [ ] 3.1 `cmd/aws.go`: branch on resolved endpoint target — skip `runtime.NewDockerRuntime`/`IsHealthy`/`container.ResolveRunningContainerName` and use the resolved URL directly when present.
- [ ] 3.2 `cmd/az.go` (`azPreflight`): same branch as 3.1; skip the `*.localhost.localstack.cloud` DNS-resolution hard requirement when an endpoint URL is given.
- [ ] 3.3 `cmd/iac.go` (`requireRunningAWSEmulator`, shared by terraform/cdk/sam): branch to the endpoint-URL path, replacing container discovery with the health/type probe and keeping the existing AWS-only error shape for non-AWS detections.
- [ ] 3.4 `internal/iac/{terraform,cdk,sam}/cli/env.go`: change `endpointURLOverride()` (currently pure value substitution reading `AWS_ENDPOINT_URL`) to delegate to the shared resolution helper (task 2.1) so that when `AWS_ENDPOINT_URL` is the only endpoint source resolved (no `--endpoint-url`/`LSTK_ENDPOINT_URL`), it also signals "skip the running-emulator check" to the command's preflight (task 3.3), consistent with every other in-scope command. Update/add tests confirming a locally-running-but-undiscovered-by-Docker scenario now succeeds and the previous "value-only, still gated" behavior is gone (call out as the BREAKING change in the PR description).
- [ ] 3.5 `cmd/snapshot.go`:
  - `save`/`load` (`resolveSnapshotDeps` callers): branch to the endpoint-URL path; disable the "auto-start via `container.Start`" fallback in `load` when an endpoint URL is resolved, failing with the unreachable-endpoint error instead.
  - `remove` (`runSnapshotRemove`): same branch as save/load.
  - `list`: branch only inside the `s3://` argument path (`snapshot.ListRemoteS3` call); when no `s3://` argument is given, leave the handler as-is — it already never reads the endpoint-url flag/env vars, so no change is needed there.
  - `show` (`runSnapshotShow`): leave as-is — it never calls `resolveSnapshotDeps` and needs no change; `--endpoint-url` is silently a no-op.
- [ ] 3.6 `cmd/reset.go`: branch to the endpoint-URL path for host resolution.
- [ ] 3.7 `cmd/status.go`: branch to the endpoint-URL path; render reachability + detected type + reported version instead of Docker-derived fields (uptime, image, bound port) when targeting an external endpoint.
- [ ] 3.8 `cmd/logs.go`, `cmd/stop.go`, `cmd/restart.go`, `cmd/volume.go`: apply the rejection helper (task 2.6) unconditionally — these are the only four commands that reject an explicit `--endpoint-url` outright.

## 4. Host/endpoint formatting reuse

- [ ] 4.1 Ensure `internal/endpoint`'s virtual-host/path-style S3 addressing logic (used by terraform/cdk provider overrides) operates correctly against an arbitrary `--endpoint-url` host, not just `127.0.0.1`/`*.localstack.cloud`.
- [ ] 4.2 Confirm `internal/emulator/*` clients (`FetchVersion`/`FetchResources`) work unchanged when constructed with an externally-resolved endpoint (no Docker-derived assumptions leak into them).

## 5. Tests

- [ ] 5.1 Unit tests for the resolution helper's precedence (flag > `LSTK_ENDPOINT_URL` > `AWS_ENDPOINT_URL` > Docker fallback) and URL validation, including that `AWS_ENDPOINT_URL` resolves identically to `LSTK_ENDPOINT_URL` for a non-AWS command (e.g. `status`/`az`).
- [ ] 5.2 Unit tests for the health/type probe classification (aws/azure/snowflake/inconclusive), using recorded/mocked HTTP responses.
- [ ] 5.3 Integration test: `lstk aws s3 ls --endpoint-url <mock server>` succeeds against a non-Docker HTTP server without a Docker daemon involved.
- [ ] 5.4 Integration test: `lstk terraform`/`cdk`/`sam` fail with the AWS-specific error when `--endpoint-url` points at a detected non-AWS emulator.
- [ ] 5.5 Integration test: `lstk terraform apply` with only `AWS_ENDPOINT_URL` set (no local Docker container running, no `--endpoint-url`/`LSTK_ENDPOINT_URL`) succeeds against a mock HTTP server — proves the breaking-change bypass behavior.
- [ ] 5.6 Integration test: `lstk snapshot load --endpoint-url <unreachable>` fails without attempting to start a local container.
- [ ] 5.7 Integration test: `lstk snapshot remove pod:x --endpoint-url <mock server> --force` succeeds without Docker.
- [ ] 5.8 Integration test: `lstk snapshot list s3://bucket/prefix --endpoint-url <mock server>` succeeds without Docker; `lstk snapshot list --endpoint-url <url>` (no `s3://` arg) and `lstk snapshot show pod:x --endpoint-url <url>` both succeed exactly as without the flag (no error, no attempt to reach `<url>`), confirming the flag is silently a no-op for these two forms.
- [ ] 5.9 Integration test: `lstk status --endpoint-url <mock server>` renders reduced (no Docker-derived) output.
- [ ] 5.9a Integration test: `lstk status` with only `AWS_ENDPOINT_URL` set (no `--endpoint-url`/`LSTK_ENDPOINT_URL`, no local Docker container) resolves against it identically to `LSTK_ENDPOINT_URL`, confirming the synonym applies outside the AWS-specific commands too.
- [ ] 5.10 Integration test: `lstk logs --endpoint-url <url>` fails with the "not supported" error; `lstk stop` with only `LSTK_ENDPOINT_URL` set in the environment proceeds against local Docker discovery unaffected.

## 6. Documentation

- [ ] 6.1 Update `README.md` and `CLAUDE.md` to document `--endpoint-url`, `LSTK_ENDPOINT_URL` (the primary documented env var), and `AWS_ENDPOINT_URL` (a compatibility synonym for existing AWS users, one precedence tier lower) — both env vars honored across every in-scope command, not just `aws`/`terraform`/`cdk`/`sam` — including which commands/subcommand forms support them and which don't, a callout that an "AWS"-named variable intentionally also affects Azure/Snowflake-targeting commands, and that emulator type is always auto-detected with no manual override.
- [ ] 6.2 Update per-command help text (`Long` descriptions) for each affected command to mention `--endpoint-url` support: `snapshot list`'s conditional (`s3://` only) support, and that `snapshot show`/bare `snapshot list` silently accept but ignore the flag (platform-only, no emulator involved) rather than erroring on it.
- [ ] 6.3 Add a changelog/release-notes entry for the `AWS_ENDPOINT_URL` breaking-change behavior on `terraform`/`cdk`/`sam` (Docker running-check is now skipped when it's the only endpoint source set).
- [ ] 6.4 Fix the `internal/snapshot/CLAUDE.md` line describing `remove`/`show` as uniformly "cloud-only" to reflect that `remove` still proxies through the running emulator while `show` (and bare `list`) never contact it at all.
- [ ] 6.5 Update `docs/structured-output.md` if `--json` output for `status`/`snapshot` gains new fields (detected type, reduced-info mode) for externally-managed endpoints.
