# sam-proxy Specification

## Purpose

Provide an `lstk sam` command that proxies the AWS SAM CLI against a running LocalStack AWS emulator, injecting the resolved LocalStack endpoint into the `sam` subprocess environment so SAM targets LocalStack directly without requiring `samlocal`/`aws-sam-cli-local`.

## Requirements
### Requirement: SAM CLI proxy command
The system SHALL provide an `lstk sam` command that forwards all of its arguments to the real AWS SAM CLI (`sam`) and, before invoking it, configures the subprocess environment to target the running LocalStack instance.

#### Scenario: Pass through SAM arguments
- **WHEN** the user runs `lstk sam deploy --stack-name my-stack --no-confirm-changeset`
- **THEN** lstk invokes the `sam` binary with `deploy --stack-name my-stack --no-confirm-changeset` intact and propagates its exit code

#### Scenario: Inject LocalStack endpoint into the SAM environment
- **WHEN** lstk runs a SAM command
- **THEN** the `sam` subprocess receives `AWS_ENDPOINT_URL` set to the resolved LocalStack endpoint, and lstk does not set `AWS_ENDPOINT_URL_S3` or any S3 path-style configuration (SAM's botocore auto-selects path-style addressing against a `localhost`/IP endpoint)

#### Scenario: Honor an explicit endpoint override
- **WHEN** `AWS_ENDPOINT_URL` is already set in the environment
- **THEN** lstk uses that value instead of the auto-resolved endpoint

#### Scenario: Honor an explicit S3 endpoint override
- **WHEN** `AWS_ENDPOINT_URL_S3` is already set in the environment
- **THEN** lstk passes it through to the `sam` subprocess unchanged (it is neither set nor stripped by lstk), so a user can override S3 addressing for an exotic case

### Requirement: Direct sam invocation with no samlocal dependency
The system SHALL invoke the real `sam` binary directly and SHALL NOT require or invoke `samlocal`/`aws-sam-cli-local`. The binary name SHALL be configurable via `LSTK_SAM_CMD`, defaulting to `sam`.

#### Scenario: Resolve the sam binary from PATH
- **WHEN** `lstk sam` runs and `sam` is on `PATH`
- **THEN** lstk locates and executes it

#### Scenario: Override the binary name
- **WHEN** `LSTK_SAM_CMD` is set to an alternative binary name
- **THEN** lstk invokes that binary instead of `sam`

#### Scenario: Missing SAM binary
- **WHEN** the configured SAM binary is not found on `PATH`
- **THEN** lstk emits an actionable error directing the user to install the AWS SAM CLI and does not attempt to run anything

### Requirement: Minimum SAM CLI version
The system SHALL require AWS SAM CLI version `1.95.0` or newer, the version from which SAM honors `AWS_ENDPOINT_URL`, because lstk points SAM at LocalStack purely through environment variables, which older SAM versions do not honor.

#### Scenario: SAM version is too old
- **WHEN** the resolved `sam` binary reports a version older than `1.95.0`
- **THEN** lstk fails with an actionable error explaining the minimum version, and does not run the command (so it cannot silently target real AWS)

#### Scenario: SAM version is supported
- **WHEN** the resolved `sam` binary reports a version at or above `1.95.0`
- **THEN** lstk proceeds to run the command

### Requirement: Mock credentials and AWS environment isolation
The system SHALL provide LocalStack-compatible credentials to the `sam` subprocess and SHALL strip ambient AWS configuration that could redirect SAM to real AWS. lstk SHALL NOT require, read, or inject the LocalStack auth token for SAM-to-LocalStack API calls; the auth token only activates the emulator container.

SAM passes `AWS_ACCESS_KEY_ID` straight through and LocalStack derives the account from it; lstk SHALL set `AWS_ACCESS_KEY_ID` to the resolved account (see "Account selection") and SHALL fix `AWS_SECRET_ACCESS_KEY=test`.

#### Scenario: Provide credentials and region
- **WHEN** lstk runs a SAM command
- **THEN** the subprocess environment contains `AWS_ACCESS_KEY_ID` set to the resolved account, `AWS_SECRET_ACCESS_KEY=test`, and the resolved region in both `AWS_REGION` and `AWS_DEFAULT_REGION`

#### Scenario: Strip ambient AWS configuration
- **WHEN** the user's environment contains `AWS_PROFILE`, `AWS_DEFAULT_PROFILE`, `AWS_SESSION_TOKEN`, or a real `AWS_SECRET_ACCESS_KEY` value
- **THEN** lstk removes or overrides them in the subprocess environment so SAM cannot resolve credentials or a profile that point at real AWS

### Requirement: Region selection
The system SHALL accept the lstk-specific `--region` flag in leading position (before the SAM subcommand) and encode it into the subprocess environment, with the same parsing and precedence as `lstk terraform` and `lstk cdk`. Because SAM honors `AWS_DEFAULT_REGION` (and not `AWS_REGION`), lstk SHALL write the resolved region to both `AWS_REGION` and `AWS_DEFAULT_REGION`.

#### Scenario: Region precedence
- **WHEN** `--region` is omitted
- **THEN** lstk resolves the region from `AWS_REGION`, falling back to `us-east-1`

#### Scenario: Region reaches SAM via AWS_DEFAULT_REGION
- **WHEN** lstk runs a SAM command with a resolved region
- **THEN** the subprocess environment sets both `AWS_REGION` and `AWS_DEFAULT_REGION` to that region, so SAM (which reads `AWS_DEFAULT_REGION`) uses it

#### Scenario: Flags only in leading position
- **WHEN** `--region` appears after the SAM subcommand (e.g. `lstk sam deploy --region us-west-2`)
- **THEN** lstk forwards it to `sam` unchanged rather than consuming it

#### Scenario: Reject a leading flag before the subcommand at the lstk boundary
- **WHEN** `--region` or `--account` appears before the `sam` subcommand on the lstk command line (e.g. `lstk --region us-west-2 sam deploy`)
- **THEN** lstk fails with an error explaining the flag must appear after the `sam` subcommand, and does not invoke `sam`

### Requirement: Account selection
The system SHALL accept the lstk-specific `--account` flag in leading position (before the SAM subcommand), mirroring `lstk terraform`, because SAM passes `AWS_ACCESS_KEY_ID` through to LocalStack which uses it as the account id. The resolved account SHALL be written to `AWS_ACCESS_KEY_ID` in the subprocess environment.

#### Scenario: Explicit account
- **WHEN** `--account 111111111111` is provided in leading position
- **THEN** lstk validates it as a 12-digit account id and sets `AWS_ACCESS_KEY_ID=111111111111` for the `sam` subprocess, so resources are created under that LocalStack account

#### Scenario: Invalid account value
- **WHEN** `--account` is provided with a value that is not exactly 12 digits
- **THEN** lstk fails at the command boundary with an error and does not invoke `sam`

#### Scenario: Account precedence and real-key deactivation
- **WHEN** `--account` is omitted
- **THEN** lstk resolves the account from the ambient `AWS_ACCESS_KEY_ID` (deactivating a real-looking `AKIA…`/`ASIA…` key so it never reaches LocalStack), falling back to `test` (default account `000000000000`)

### Requirement: Emulator gating for AWS-contacting commands
The system SHALL require a running AWS emulator for SAM subcommands that contact AWS APIs and SHALL run a fixed set of offline subcommands without that requirement.

#### Scenario: AWS-contacting command without a running emulator
- **WHEN** the user runs an AWS-contacting subcommand (e.g. `lstk sam deploy`) and the AWS emulator is not running
- **THEN** lstk emits an actionable "LocalStack is not running" error (with a command to start it) and does not invoke `sam`

#### Scenario: A different emulator is running
- **WHEN** an AWS-contacting SAM command is run while a non-AWS emulator (e.g. Snowflake or Azure) is running but the AWS emulator is not
- **THEN** lstk fails with an AWS-specific error naming the running emulator rather than a misleading generic "not running" message

#### Scenario: Offline command without a running emulator
- **WHEN** the user runs an offline subcommand (e.g. `lstk sam init`, `lstk sam build`, `lstk sam validate`, `lstk sam local generate-event`)
- **THEN** lstk runs it without requiring a running emulator

### Requirement: Streamed passthrough output
The system SHALL stream the SAM subprocess's stdin, stdout, and stderr through unobstructed and SHALL NOT display a spinner or capture SAM output into lifecycle events.

#### Scenario: Unobstructed streaming
- **WHEN** a long-running SAM command (e.g. `lstk sam deploy`) produces incremental output
- **THEN** lstk streams that output directly to the terminal without a spinner or reformatting, and forwards interactive prompts via stdin

#### Scenario: Propagate failure
- **WHEN** the SAM command exits non-zero
- **THEN** lstk returns a silent error carrying that exit status so the top-level handler does not reprint it

### Requirement: Known limitations versus samlocal
Because `lstk sam` runs the real `sam` as a subprocess and configures it only through environment variables (rather than monkeypatching SAM internals like the Python `samlocal` wrapper), two `samlocal` behaviors are not guaranteed in this version: image/container-based Lambda (ECR) deploys and nested CloudFormation stack template export. The system SHALL surface these limitations to the user in the command help text and SHALL point users to `samlocal` as the fallback for those workflows. The system SHALL NOT silently alter or block these commands beyond the normal emulator gating — they are forwarded to `sam` like any other command.

#### Scenario: Limitations documented in help
- **WHEN** the user runs `lstk sam --help`
- **THEN** the help text notes that image/container-based Lambda (ECR) deploys and nested CloudFormation stacks are not fully supported and that `samlocal` is the fallback for those workflows

#### Scenario: Unsupported workflow is still forwarded, not blocked
- **WHEN** the user runs an image-based Lambda deploy (e.g. `lstk sam deploy` for a template with `PackageType: Image`)
- **THEN** lstk forwards the command to `sam` unchanged (subject to the normal emulator gate) rather than rejecting it, so the user sees SAM's own behavior/errors
