## Why

Users running AWS CDK against LocalStack today must install LocalStack's separate Node wrapper, `cdklocal` (the `aws-cdk-local` npm package), which depends on a matching `aws-cdk` install and points CDK's API calls at LocalStack. lstk already proxies the AWS CLI via `lstk aws` and Terraform via `lstk terraform`; extending the same pattern to CDK lets users run `lstk cdk` with no extra wrapper and with the LocalStack endpoint resolved automatically from the running emulator.

## What Changes

- Add a new `lstk cdk <args>` command that proxies all arguments to the real `cdk` binary, mirroring the passthrough behavior of `lstk aws`.
- Before invoking `cdk`, build a clean AWS environment for the subprocess that points CDK at the running LocalStack instance: set `AWS_ENDPOINT_URL` (and an `.s3.`-prefixed `AWS_ENDPOINT_URL_S3`) to the resolved LocalStack endpoint, set mock credentials and region, and **strip ambient AWS configuration** (`AWS_PROFILE`, `AWS_DEFAULT_PROFILE`, `AWS_SESSION_TOKEN`, and any real `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`) so a stray profile cannot silently redirect a deploy at real AWS.
- Resolve the LocalStack endpoint automatically via lstk's existing container discovery + `endpoint.ResolveHost`, instead of the `LOCALSTACK_HOSTNAME`/`EDGE_PORT` environment variables `cdklocal` relies on.
- Invoke the **real `cdk` binary directly** (configurable via `LSTK_CDK_CMD`, default `cdk`) and reimplement `cdklocal`'s endpoint setup natively in Go — exactly as `lstk terraform` invokes `terraform` rather than `tflocal`. lstk does **not** depend on `cdklocal` or `aws-cdk-local`.
- Require **AWS CDK >= 2.177.0**. From that version the CDK CLI honors `AWS_ENDPOINT_URL`/`AWS_ENDPOINT_URL_S3`, so the endpoint can be set entirely through environment variables. Earlier versions only worked via `cdklocal`'s in-process Node SDK monkey-patching, which a Go subprocess cannot reproduce; lstk fails fast with an actionable error rather than silently deploying to real AWS.
- Support lstk-specific `--region <region>` and `--account <id>` flags (leading position, before the CDK action), reusing the same parsing/validation/precedence as `lstk terraform` (including the `AWS_ACCESS_KEY_ID` access-key deactivation safeguard).
- Gate AWS-contacting subcommands (`bootstrap`, `deploy`, `destroy`, `diff`, `import`, `watch`, `rollback`, …) on a running AWS emulator, emitting the same actionable "not running" error as `lstk terraform`. A fixed set of offline subcommands (`init`, `synth`, `ls`, `version`, `doctor`, `acknowledge`/`ack`, `context`, `notices`) runs without requiring the emulator.
- Stream `cdk`'s stdin/stdout/stderr through unobstructed and propagate its exit code. **No spinner**, no lifecycle-event capture — `cdk deploy`/`bootstrap` are long-running, streaming commands whose own output must remain intact (same decision as `lstk terraform`).
- Support a small environment-variable surface: `LSTK_CDK_CMD`, `AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3`, `AWS_REGION`, and `AWS_ACCESS_KEY_ID`.

## Capabilities

### New Capabilities
- `cdk-proxy`: Running AWS CDK against a running LocalStack instance through `lstk`, including automatic endpoint resolution, a clean LocalStack-pointed AWS environment for the `cdk` subprocess, mock credentials with region/account selection, emulator-gating of AWS-contacting commands, and a defined set of supported environment variables.

### Modified Capabilities
<!-- None: no existing specs change. -->

## Impact

- **New command wiring**: `cmd/cdk.go` (Cobra command), registered in the root command alongside `newAWSCmd`/`newTerraformCmd`.
- **New domain package**: `internal/iac/cdk/cli/` (Go `package cli`, imported as `cdkcli`) for binary discovery, version checking, AWS-environment construction, and subprocess execution. Lives under the existing `internal/iac/` umbrella next to `internal/iac/terraform/`.
- **Reused infrastructure**: `internal/container` (`ResolveRunningContainerName`, `RunningEmulators`), `internal/endpoint` (`ResolveHost`), `internal/runtime` (Docker health), `internal/output` (sink/events), `internal/config` (AWS emulator container resolution). The `--region`/`--account` parsing, validation, and `DeactivateAccessKey` safeguard from the terraform command are shared rather than duplicated.
- **External dependency**: requires the `cdk` binary (AWS CDK CLI >= 2.177.0) on `PATH`. No new Go module dependencies.
- **Docs**: update `README.md` and `CLAUDE.md` to document the new command, its supported environment variables, and the CDK >= 2.177.0 requirement.
