## Why

LocalStack users who build serverless applications with the AWS SAM CLI (`sam`) currently have to use the third-party `samlocal` wrapper (the `aws-sam-cli-local` pip package) to point SAM at LocalStack. lstk already provides first-party `lstk terraform` and `lstk cdk` proxies that remove the need for `tflocal`/`cdklocal`; adding `lstk sam` closes the gap for the SAM ecosystem so SAM users get the same zero-config, no-extra-dependency experience from the lstk CLI itself.

## What Changes

- Add a new `lstk sam` command that forwards all of its arguments to the real AWS SAM CLI (`sam`) and, before invoking it, configures the subprocess environment to target the running LocalStack instance — using CDK's env-var mechanism but Terraform's account model (see design.md).
- Inject `AWS_ENDPOINT_URL` so SAM's underlying botocore clients route CloudFormation/S3/Lambda/STS/IAM calls (`sam deploy`, `package`, `sync`, `delete`, `logs`, …) to LocalStack. Testing confirmed SAM needs no S3-specific endpoint — botocore auto-selects path-style addressing against a `localhost`/IP endpoint — so `AWS_ENDPOINT_URL_S3` is only honored as an override, never set by lstk.
- Provide LocalStack-compatible mock credentials and strip ambient AWS configuration (`AWS_PROFILE`, `AWS_DEFAULT_PROFILE`, `AWS_SESSION_TOKEN`, real keys) so a SAM deploy can never silently target real AWS. `AWS_SECRET_ACCESS_KEY` is fixed to `test`.
- Accept the lstk-specific `--region` flag in leading position with the same parsing/precedence as `lstk terraform`/`lstk cdk` (`--region` → `AWS_REGION` → `us-east-1`), and write the resolved region to both `AWS_REGION` and `AWS_DEFAULT_REGION` in the subprocess (SAM honors `AWS_DEFAULT_REGION`, not `AWS_REGION`).
- Support the lstk-specific `--account` flag, mirroring `lstk terraform`: SAM passes `AWS_ACCESS_KEY_ID` straight through and LocalStack maps it to the account (confirmed by testing with a custom account id). The resolved account is written to `AWS_ACCESS_KEY_ID`; absent a flag it falls back to a deactivated ambient `AWS_ACCESS_KEY_ID`, then `test` (default account `000000000000`).
- Require a running AWS emulator for AWS-contacting subcommands; run a fixed set of offline/local subcommands (`init`, `build`, `validate`, `local …`, `local generate-event`, `version`, …) without that requirement.
- Require AWS SAM CLI `1.95.0` or newer (the version from which SAM honors `AWS_ENDPOINT_URL`), refusing to run older versions so they cannot silently target real AWS (mirrors the CDK version gate).
- Resolve the `sam` binary from `PATH`, configurable via `LSTK_SAM_CMD` (default `sam`); emit an actionable error when it is missing. Do **not** require or invoke the third-party `samlocal`/`aws-sam-cli-local`.
- Stream the SAM subprocess's stdin/stdout/stderr through unobstructed and propagate its exit code.
- Register the command with the root command and document it in `CLAUDE.md`.

**Scope note — parity with `samlocal`:** `lstk sam` is a Go subprocess wrapper, whereas `samlocal` is a Python script that monkeypatches SAM's internals in-process. `lstk sam` is functionally equivalent for the common case (ZIP-based Lambdas + standard CloudFormation/S3/IAM/STS operations), but two `samlocal` behaviors are **not covered in v1** because they require in-process patches no env var or flag can reach: **image/container-based Lambda (ECR) deploys** and **nested CloudFormation stack template export**. These are documented as known limitations (with `samlocal` as the fallback) and tracked as Open Questions in design.md for a possible follow-up.

## Capabilities

### New Capabilities
- `sam-proxy`: An `lstk sam` command that proxies the AWS SAM CLI against the running LocalStack emulator by configuring the subprocess environment (endpoint, mock credentials, AWS-config isolation, region), gating AWS-contacting subcommands on a running emulator, and enforcing a minimum SAM CLI version.

### Modified Capabilities
<!-- None: this is a new, self-contained command with no existing spec-level behavior changing. -->

## Impact

- **New code**: `cmd/sam.go` (Cobra wiring) and `internal/iac/sam/cli/` (domain logic: `exec.go`, `env.go`, `version.go`, `defaults.go`), mirroring `internal/iac/cdk/cli/`.
- **Reused code**: `cmd/iac.go` shared helpers (`stripLeadingIaCFlags`, `resolveRegion`, `resolveAccount`, `requireRunningAWSEmulator`, `rejectPreSubcommandFlags`, `emitValidationError`, `resolveAWSContainer`), `internal/iac/terraform/cli` (`DeactivateAccessKey`, via `resolveAccount`), `internal/endpoint` (`ResolveHost`), `internal/output`, `internal/runtime`. SAM does not need `endpoint.S3Addressing`.
- **Modified code**: `cmd/root.go` (register `newSamCmd`), `CLAUDE.md` (document the command).
- **New env vars** (public interface): `LSTK_SAM_CMD` (binary override); `AWS_ENDPOINT_URL` and `AWS_ENDPOINT_URL_S3` honored as overrides (only `AWS_ENDPOINT_URL` is set by lstk); `AWS_REGION` as `--region` fallback.
- **Tests**: unit tests for SAM env building and version parsing; integration tests with a fake `sam` binary (arg forwarding, env injection, version gate, offline gating); optional e2e tests against a real `sam` + LocalStack.
- **External dependency**: relies on a sufficiently recent AWS SAM CLI on the user's `PATH`; no new Go module dependencies.
