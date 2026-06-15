## 1. Domain logic — `internal/iac/sam/cli/`

(Pre-implementation verification is done: minimum version `1.95.0`; offline classification established empirically against SAM CLI 1.151.0 — see design Decision 4. The gate is UX-only since the env always targets LocalStack.)

- [x] 1.1 Create `env.go`: `samCmd()` reading `LSTK_SAM_CMD` (default `sam`), `endpointURLOverride()` reading `AWS_ENDPOINT_URL`, and `s3EndpointOverride()` reading `AWS_ENDPOINT_URL_S3` (only honored as a user override — never set by lstk; see Decision 2).
- [x] 1.2 Create `defaults.go`: minimum-version constants (`minSAMMajor=1`, `minSAMMinor=95`, `minSAMPatch=0`, `minSAMVersionString="1.95.0"`); the top-level `offlineCommands` set `{docs, init, build, validate, local, pipeline}` (everything else is AWS-contacting; no second-level classification — see Decision 4); the `valueFlags` set for value-taking SAM global flags; and the `IsOffline`/`subcommand` scanner (mirror `internal/iac/cdk/cli/defaults.go`).
- [x] 1.3 Create `version.go`: `CheckVersion(ctx, samBin)` running `sam --version`, parsing `MAJOR.MINOR.PATCH`, and returning an actionable error below `1.95.0` (mirror `internal/iac/cdk/cli/version.go`).
- [x] 1.4 Create `env.go` `BuildEnv(base, endpointURL, account, region)`: set `AWS_ENDPOINT_URL`, `AWS_ACCESS_KEY_ID=account`, `AWS_SECRET_ACCESS_KEY=test`, and **both** `AWS_REGION` and `AWS_DEFAULT_REGION` to the resolved region (do **not** set `SAM_CLI_TELEMETRY` — leave the user's telemetry preference untouched, per design Decision 2); strip `AWS_PROFILE`, `AWS_DEFAULT_PROFILE`, `AWS_SESSION_TOKEN`; do **not** set or strip `AWS_ENDPOINT_URL_S3` (pass-through escape hatch); skip empty managed values. No S3 endpoint or path-style handling.
- [x] 1.5 Create `exec.go` `Run(ctx, endpointURL, account, region, sink, logger, args)`: locate binary (emit actionable `ErrorEvent` directing to install the AWS SAM CLI + silent error if missing), `CheckVersion`, apply the `AWS_ENDPOINT_URL` override if set, build env via `BuildEnv`, exec `sam` with stdin/stdout/stderr wired through, wrap non-zero exit as `output.NewSilentError`. Add the OTEL span/attributes like CDK. Does not import `endpoint.S3Addressing`.

## 2. Command wiring — `cmd/sam.go`

- [x] 2.1 Add `newSamCmd(cfg *env.Env, logger log.Logger)` with `Use: "sam [args...]"`, `DisableFlagParsing: true`, and `PreRunE` that calls `stripGlobalFlags` + `initConfig` (mirror `cmd/cdk.go`).
- [x] 2.2 In `RunE`: `rejectPreSubcommandFlags(cmd.CalledAs())`, `stripLeadingIaCFlags(passthrough, false)`, `resolveRegion(regionFlag)`, `resolveAccount(accountFlag)` (mirror `cmd/terraform.go` — `--account` is supported; surface a validation error via `emitValidationError` on an invalid account), `resolveAWSContainer`.
- [x] 2.3 For offline subcommands (`samcli.IsOffline`), resolve host (DNS only) and call `samcli.Run` without requiring a running emulator.
- [x] 2.4 For AWS-contacting subcommands: create the Docker runtime, check `IsHealthy`, `requireRunningAWSEmulator(..., "sam")`, resolve host, then call `samcli.Run`. No DNS-fallback warning is needed (SAM uses path-style addressing on any resolved host).
- [x] 2.5 Write the `Short`/`Long` help text as unbroken paragraphs (per CLAUDE.md), covering the leading `--region` and `--account` flags, the minimum SAM version (`1.95.0`), the supported env vars (`AWS_ENDPOINT_URL`, `LSTK_SAM_CMD`, `AWS_REGION`; note `AWS_ENDPOINT_URL_S3` is honored only as an override), and a brief known-limitations note that image/container-based Lambda (ECR) deploys and nested CloudFormation stacks are not supported — use `samlocal` for those.
- [x] 2.6 Register `newSamCmd(cfg, logger)` in `cmd/root.go` alongside `newCDKCmd`.

## 3. Tests

- [x] 3.1 Unit test `BuildEnv` (`internal/iac/sam/cli/env_test.go`): `AWS_ENDPOINT_URL` set, no `AWS_ENDPOINT_URL_S3` set, `AWS_ACCESS_KEY_ID` = the passed account, `AWS_SECRET_ACCESS_KEY=test`, both region vars set, AWS-config stripping, user-set `AWS_ENDPOINT_URL_S3` passed through untouched, empty-value skipping.
- [x] 3.2 Unit test `CheckVersion` (`version_test.go`): parsing and comparison against `1.95.0` (e.g. `1.94.x` fails, `1.95.0`/`1.96.x` pass).
- [x] 3.3 Unit test `IsOffline`/`subcommand` (`defaults_test.go`): offline tokens (`docs`, `init`, `build`, `validate`, `local`, `pipeline`) vs AWS-contacting (`deploy`, `sync`, `package`, `delete`, `logs`, `traces`, `list`, `remote`, `publish`); flag and value-flag skipping; two-level commands resolve to their top-level token (e.g. `local generate-event` → `local` offline, `list resources` → `list` AWS-contacting).
- [x] 3.4 Integration test with a fake `sam` binary (`test/integration/sam_cmd_test.go`, mirror `cdk_cmd_test.go`): arg forwarding, env injection (incl. `AWS_DEFAULT_REGION` and a custom `--account` reaching `AWS_ACCESS_KEY_ID`), version gate, missing-binary error, offline command runs without a running emulator. Use `testEnvWithHome(t.TempDir(), "")`; mark `t.Parallel()` where no Docker state is shared.
- [x] 3.5 (Optional) e2e test against a real `sam` + LocalStack (`test/integration/sam_e2e_test.go`) with a minimal SAM sample under `test/integration/test-samples/iac/sam/`: `validate`/`build` offline, a `deploy` round-trip, and a `deploy` with a custom `--account` asserting resources land under that account.

## 4. Documentation

- [x] 4.1 Document `lstk sam` in `CLAUDE.md` (alongside the IaC/terraform/cdk descriptions): purpose, that no `samlocal` install is needed, the minimum SAM version (`1.95.0`), `--region`/`--account` support, the supported env vars, and the known limitations vs `samlocal` (no image/container-based Lambda or nested-stack support — use `samlocal` for those).

## 5. Verification

- [x] 5.1 Run `make lint`, `make test`, and `make test-integration RUN=...` for the new SAM tests; confirm all pass.
- [x] 5.2 Manually verify `lstk sam --help`, `lstk sam validate` (offline), a `lstk sam deploy` against a running AWS emulator routing to LocalStack, and a `lstk sam deploy --account 111111111111` landing resources under that account. (Verified locally: `lstk help sam` renders the help — note `lstk sam --help` forwards under `DisableFlagParsing` exactly like `lstk cdk`, so `lstk help sam` is the way to read it — and `lstk sam validate` runs offline against the real sam 1.151.0. The `deploy`/`--account` paths are covered by the gated e2e tests `TestSAME2EDeployDelete`/`TestSAME2EDeployCustomAccount`, which skip locally without `LOCALSTACK_AUTH_TOKEN` and run in CI; the env-injection contract those rely on is asserted by `TestSAMInjectsCleanAWSEnv`/`TestSAMAccountSupported`.)
