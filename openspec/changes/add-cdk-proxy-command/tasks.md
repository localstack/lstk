## 1. Domain package scaffolding

- [x] 1.1 Create `internal/iac/cdk/cli/` package (Go `package cli`, imported as `cdkcli`) with a `Run(ctx context.Context, endpointURL, region, account string, sink output.Sink, logger log.Logger, args []string) error` entry-point signature
- [x] 1.2 Add `defaults.go`: the minimum-supported-version constant (`2.177.0`) and the fixed offline-subcommand set (`init`, `synth`/`synthesize`, `ls`/`list`, `version`, `doctor`, `acknowledge`/`ack`, `context`, `notices`) with an `IsOffline(args []string) bool` helper that resolves the first non-flag token (mirror terraform's `IsUnproxied`/`subcommand`). The set is fixed, not env-configurable
- [x] 1.3 Add `env.go` with package-scoped env helpers: `cdkCmd()` (`LSTK_CDK_CMD`, default `cdk`), `endpointURLOverride()` (`AWS_ENDPOINT_URL`), and `s3EndpointOverride()` (`AWS_ENDPOINT_URL_S3`). Do NOT read `LOCALSTACK_HOSTNAME`/`EDGE_PORT`/`AWS_DEFAULT_REGION`

## 2. AWS environment construction

- [x] 2.1 Implement `BuildEnv(base []string, endpointURL, s3Endpoint, region, account string) []string`: start from `base` (`os.Environ()`), then SET `AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3`, `AWS_ACCESS_KEY_ID` (account), `AWS_SECRET_ACCESS_KEY=test`, `AWS_REGION`, and `AWS_DEFAULT_REGION` (overriding any existing values), and optionally `CDK_DISABLE_LEGACY_EXPORT_WARNING=1`
- [x] 2.2 In `BuildEnv`, REMOVE ambient AWS config that could redirect CDK at real AWS: `AWS_PROFILE`, `AWS_DEFAULT_PROFILE`, `AWS_SESSION_TOKEN` (and ensure the mock key/secret override any real `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`)
- [x] 2.3 Derive the S3 endpoint: reuse/extract terraform's `s3Addressing`/`virtualHostCapable` logic so a virtual-host-capable `*.localstack.cloud` host gets an `s3.`-prefixed endpoint and `127.0.0.1`/`localhost` gets the bare endpoint; let `AWS_ENDPOINT_URL_S3` override the derivation. Factor the shared helper into a location both `tfcli` and `cdkcli` can use rather than duplicating it
- [x] 2.4 Unit-test `BuildEnv`: LocalStack values are set; `AWS_PROFILE`/`AWS_DEFAULT_PROFILE`/`AWS_SESSION_TOKEN` are stripped; a real `AWS_ACCESS_KEY_ID` in `base` is overridden; the `s3.` prefix appears for virtual-host hosts and not for `127.0.0.1`/`localhost`; `AWS_ENDPOINT_URL`/`AWS_ENDPOINT_URL_S3` overrides win

## 3. Version checking

- [x] 3.1 Implement `version.go` `CheckVersion(ctx context.Context, cdkBin string) error`: run `<cdkBin> --version`, parse the leading `MAJOR.MINOR.PATCH`, and return an actionable error when the version is below `2.177.0`
- [x] 3.2 Fail safe on unparseable `--version` output (treat as too old) with a clear error mentioning the minimum version
- [x] 3.3 Unit-test the version parse/compare: `2.177.0` and newer pass; `2.176.x` and older fail; malformed output fails

## 4. Execution orchestration

- [x] 4.1 In `exec.go`, resolve the binary via `exec.LookPath(cdkCmd())`; on failure return a clear "install the AWS CDK CLI" error
- [x] 4.2 Call `CheckVersion` before running; abort with the actionable error when it fails (so an old CDK can never silently target real AWS)
- [x] 4.3 Build the subprocess environment via `BuildEnv` and run `cdk` via `exec.CommandContext` with `os.Stdin` and the passed stdout/stderr writers; wrap non-zero exit as `output.NewSilentError`; do NOT wrap output in a spinner writer
- [x] 4.4 Add an OpenTelemetry span around execution (mirror `internal/awscli/exec.go`), recording the args and exit code

## 5. Region/account flag handling (shared with terraform)

- [x] 5.1 Extract the `--region`/`--account` leading-flag parsing, validation (`accountIDRe`, 12-digit), precedence (`resolveRegion`/`resolveAccount`), `DeactivateAccessKey`, `rejectPreSubcommandFlags`, and `emitValidationError` from `cmd/terraform.go` into a form both `terraform` and `cdk` call (do NOT duplicate). The CDK stripper extracts only `--region`/`--account` (no `-chdir`)
- [x] 5.2 Resolve effective region (`--region` → `AWS_REGION` → `us-east-1`) and account (`--account` → `AWS_ACCESS_KEY_ID` → `test`, run through `DeactivateAccessKey`) at the cmd boundary and pass into `cdkcli.Run`
- [x] 5.3 Unit-test the shared parser still behaves for terraform (regression) and for cdk: both flag forms, missing value, invalid account, env fallback, leading-only behavior (flags after the subcommand forwarded verbatim)

## 6. Command wiring

- [x] 6.1 Add `cmd/cdk.go` with `newCDKCmd(cfg *env.Env, logger log.Logger)`: `Use: "cdk [args...]"`, `DisableFlagParsing: true`, `PreRunE` = `stripGlobalFlags` + `initConfig(nil)`, and `Long` help documenting `--region`/`--account`, the supported env vars, the CDK >= 2.177.0 requirement, and that LocalStack must be running for AWS-contacting commands
- [x] 6.2 In `RunE`: reject pre-subcommand `--region`/`--account`, strip+resolve the flags, then for offline subcommands (`IsOffline`) call `cdkcli.Run` with an empty endpoint and no emulator gating
- [x] 6.3 For AWS-contacting subcommands, reuse the terraform preamble (`runtime.NewDockerRuntime`, `resolveAWSContainer`, `rt.IsHealthy`/`EmitUnhealthyError`, `container.ResolveRunningContainerName`, the `runningNonAWSEmulator` AWS-specific error, `endpoint.ResolveHost`), then call `cdkcli.Run` with `http://host:port`
- [x] 6.4 When `endpoint.ResolveHost` returns the `127.0.0.1` DNS-rebind fallback, emit a `SeverityWarning` note that CDK S3 asset operations may not work on the loopback host and suggest ensuring `localhost.localstack.cloud` resolves (or setting `AWS_ENDPOINT_URL`/`AWS_ENDPOINT_URL_S3`)
- [x] 6.5 Register the command in the root command alongside `newAWSCmd`/`newTerraformCmd`

## 7. Tests

- [x] 7.1 Integration test: `lstk cdk <args>` forwards args and propagates exit code, using a stub `cdk` binary on `PATH` (that also answers `--version` with a >= 2.177.0 string) and an isolated `$HOME` via `testEnvWithHome`
- [x] 7.2 Integration test: the stub `cdk` records its environment; assert `AWS_ENDPOINT_URL`/`AWS_ENDPOINT_URL_S3`/`AWS_REGION`/mock creds are set and `AWS_PROFILE`/`AWS_DEFAULT_PROFILE`/`AWS_SESSION_TOKEN` are stripped
- [x] 7.3 Integration test: an AWS-contacting command (e.g. `deploy`) fails with the "not running" error and does not invoke `cdk` when no emulator is running; and fails with the AWS-specific message when a non-AWS emulator is running
- [x] 7.4 Integration test: offline commands (`init`/`synth`/`ls`/`version`/`doctor`) run without requiring a running emulator (including with leading `--region`/`--account` present, which are stripped)
- [x] 7.5 Integration test: a stub `cdk` reporting a version below 2.177.0 causes a clear failure before the command runs; a missing `cdk` binary yields the install error
- [x] 7.6 Integration test: leading `--region`/`--account` are stripped and reflected in the subprocess env; invalid `--account` fails before `cdk` is invoked; `--region`/`--account` after the subcommand are forwarded to `cdk` unchanged; `lstk --account … cdk` (before the subcommand) is rejected
- [x] 7.7 Integration test: `LSTK_CDK_CMD` causes lstk to invoke the alternative binary stub instead of `cdk`

## 8. End-to-end (real cdk + LocalStack)

- [x] 8.1 Harness: a Docker+token+cdk-gated helper that brings up a real LocalStack via `lstk start` (named so name-based discovery finds it, 4566 on 127.0.0.1) and a readable sample CDK app under `test/integration/test-samples/iac/cdk/<name>/`, copied into a temp dir per test (gitignore the CDK `cdk.out`/asset artifacts). Skip when Docker, a real `cdk` (>= 2.177.0), or the auth token is absent
- [x] 8.2 E2E: `lstk cdk bootstrap` succeeds against LocalStack
- [x] 8.3 E2E: `lstk cdk deploy --require-approval never` of a minimal stack (e.g. a single S3 bucket or SSM parameter) succeeds against LocalStack and `lstk cdk destroy --force` tears it down
- [x] 8.4 E2E: `lstk cdk synth` of the sample app succeeds without a running emulator (offline path)
- [x] 8.5 CI: install the AWS CDK CLI (>= 2.177.0) on the Linux integration shards; skip on shards where it is unavailable. Raise the integration timeout if the CDK bootstrap/deploy + image pull pushes the suite past the current budget
- [x] 8.6 New sample `test/integration/test-samples/iac/cdk/lambda-asset/`: a stack with a `lambda.Function` whose code is `lambda.Code.fromAsset("lambda")` pointing at a real `lambda/index.js` Node handler. It MUST use `fromAsset`, not `fromInline` (inline embeds the code in the template and never produces an S3 asset, defeating the purpose). Reuses the existing `copyCDKSample`/`npmInstall` helpers and gitignores `cdk.out`/`node_modules`/asset artifacts like `single-bucket`. Validated with `cdk synth`: the synthesized `AWS::Lambda::Function` references `S3Bucket: cdk-hnb659fds-assets-…` + `S3Key: <hash>.zip` (confirming a real S3 asset, not inline code)
- [x] 8.7 E2E (separate test case `TestCDKE2ELambdaAssetDeployDestroy`, distinct from the bucket deploy): full Lambda round-trip against LocalStack — `lstk cdk bootstrap`, `lstk cdk deploy --require-approval never`, then `lstk cdk destroy --force`. Unlike the bucket-only stack (whose small template may be passed inline to CloudFormation), a `fromAsset` Lambda guarantees a real `PutObject` across `AWS_ENDPOINT_URL_S3` at deploy time, so deploy success is itself the assertion that the asset path routed through LocalStack — hardening that guarantee from "probably" to "definitely". When the real `aws` CLI is on `PATH` (e.g. GitHub-hosted runners) the test additionally invokes the function via `lstk aws lambda invoke` and asserts the handler output, proving end-to-end provisioning from the uploaded asset; that step is skipped (deploy still runs) where `aws` is absent, since the suite does not otherwise depend on the real CLI and there is no `aws-sdk-go` dependency. Gated identically (Docker + real `cdk` + npm + token) and serialized with the other container e2e tests via `cleanup()` (no `t.Parallel()`); the Lambda runtime image pull makes the raised timeout in 8.5 non-optional. The test comment is precise that it is the CLI's asset *publish* that exercises lstk's injected S3 endpoint, not LocalStack's internal read of the asset

## 9. Documentation

- [x] 9.1 Command `Long` help: document `--region`/`--account`, the supported env vars (`LSTK_CDK_CMD`, `AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`), the CDK >= 2.177.0 requirement, and that LocalStack must be running for AWS-contacting commands. Write `Short`/`Long` as unbroken paragraphs per the CLI help-text convention
- [x] 9.2 Add a "CDK Integration" section to `README.md` (mirroring the Terraform section) and note the new command + IaC umbrella in `CLAUDE.md`
