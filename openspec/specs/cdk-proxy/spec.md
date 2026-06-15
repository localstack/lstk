# cdk-proxy Specification

## Purpose

Provide an `lstk cdk` command that proxies the AWS CDK CLI against a running LocalStack AWS emulator, injecting the resolved LocalStack endpoints into the `cdk` subprocess environment so the CDK targets LocalStack directly without requiring `cdklocal`/`aws-cdk-local`.

## Requirements
### Requirement: CDK CLI proxy command
The system SHALL provide an `lstk cdk` command that forwards all of its arguments to the real AWS CDK CLI (`cdk`) and, before invoking it, configures the subprocess environment to target the running LocalStack instance.

#### Scenario: Pass through CDK arguments
- **WHEN** the user runs `lstk cdk deploy MyStack --require-approval never`
- **THEN** lstk invokes the `cdk` binary with `deploy MyStack --require-approval never` intact and propagates its exit code

#### Scenario: Inject LocalStack endpoint into the CDK environment
- **WHEN** lstk runs a CDK command
- **THEN** the `cdk` subprocess receives `AWS_ENDPOINT_URL` set to the resolved LocalStack endpoint and `AWS_ENDPOINT_URL_S3` set to the corresponding S3 endpoint (with an `s3.` host prefix when the host is virtual-host-capable)

#### Scenario: Honor an explicit endpoint override
- **WHEN** `AWS_ENDPOINT_URL` is already set in the environment
- **THEN** lstk uses that value instead of the auto-resolved endpoint

### Requirement: Direct cdk invocation with no cdklocal dependency
The system SHALL invoke the real `cdk` binary directly and SHALL NOT require or invoke `cdklocal`/`aws-cdk-local`. The binary name SHALL be configurable via `LSTK_CDK_CMD`, defaulting to `cdk`.

#### Scenario: Resolve the cdk binary from PATH
- **WHEN** `lstk cdk` runs and `cdk` is on `PATH`
- **THEN** lstk locates and executes it

#### Scenario: Override the binary name
- **WHEN** `LSTK_CDK_CMD` is set to an alternative binary name
- **THEN** lstk invokes that binary instead of `cdk`

#### Scenario: Missing CDK binary
- **WHEN** the configured CDK binary is not found on `PATH`
- **THEN** lstk emits an actionable error directing the user to install the AWS CDK CLI and does not attempt to run anything

### Requirement: Minimum CDK version
The system SHALL require AWS CDK CLI version 2.177.0 or newer, because lstk points CDK at LocalStack purely through environment variables (`AWS_ENDPOINT_URL`/`AWS_ENDPOINT_URL_S3`), which older CDK versions do not honor.

#### Scenario: CDK version is too old
- **WHEN** the resolved `cdk` binary reports a version older than 2.177.0
- **THEN** lstk fails with an actionable error explaining the minimum version, and does not run the command (so it cannot silently target real AWS)

### Requirement: Mock credentials and AWS environment isolation
The system SHALL provide LocalStack-compatible mock credentials to the `cdk` subprocess and SHALL strip ambient AWS configuration that could redirect CDK to real AWS. lstk SHALL NOT require, read, or inject the LocalStack auth token for CDK-to-LocalStack API calls; the auth token only activates the emulator container.

CDK always operates against the default LocalStack account `000000000000`; lstk SHALL set a fixed mock `AWS_ACCESS_KEY_ID=test` and SHALL NOT derive the account from a flag or from the ambient `AWS_ACCESS_KEY_ID`.

#### Scenario: Provide mock credentials and region
- **WHEN** lstk runs a CDK command
- **THEN** the subprocess environment contains `AWS_ACCESS_KEY_ID=test`, `AWS_SECRET_ACCESS_KEY=test`, and the resolved region in `AWS_REGION`/`AWS_DEFAULT_REGION`

#### Scenario: Strip ambient AWS configuration
- **WHEN** the user's environment contains `AWS_PROFILE`, `AWS_DEFAULT_PROFILE`, `AWS_SESSION_TOKEN`, or real `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` values
- **THEN** lstk removes or overrides them in the subprocess environment so CDK cannot resolve credentials or a profile that point at real AWS

#### Scenario: A 12-digit AWS_ACCESS_KEY_ID does not change the account
- **WHEN** the user's environment contains a 12-digit `AWS_ACCESS_KEY_ID` (an account id)
- **THEN** lstk overrides it with `test` so CDK still operates against the default account `000000000000`

### Requirement: Region selection
The system SHALL accept the lstk-specific `--region` flag in leading position (before the CDK subcommand) and encode it into the subprocess environment, with the same parsing and precedence as `lstk terraform`. The system SHALL NOT accept an `--account` flag for CDK.

#### Scenario: Region precedence
- **WHEN** `--region` is omitted
- **THEN** lstk resolves the region from `AWS_REGION`, falling back to `us-east-1`

#### Scenario: Reject the --account flag
- **WHEN** `--account` is provided to `lstk cdk` in leading position, with any value
- **THEN** lstk fails at the command boundary with an error explaining that `--account` is not supported and that CDK always uses the default LocalStack account `000000000000`, and does not invoke `cdk`

#### Scenario: Flags only in leading position
- **WHEN** `--region` appears after the CDK subcommand (e.g. `lstk cdk deploy --region us-west-2`)
- **THEN** lstk forwards it to `cdk` unchanged rather than consuming it

### Requirement: Emulator gating for AWS-contacting commands
The system SHALL require a running AWS emulator for CDK subcommands that contact AWS APIs and SHALL run a fixed set of offline subcommands without that requirement.

#### Scenario: AWS-contacting command without a running emulator
- **WHEN** the user runs an AWS-contacting subcommand (e.g. `lstk cdk deploy`) and the AWS emulator is not running
- **THEN** lstk emits an actionable "LocalStack is not running" error (with a command to start it) and does not invoke `cdk`

#### Scenario: A different emulator is running
- **WHEN** an AWS-contacting CDK command is run while a non-AWS emulator (e.g. Snowflake or Azure) is running but the AWS emulator is not
- **THEN** lstk fails with an AWS-specific error naming the running emulator rather than a misleading generic "not running" message

#### Scenario: Offline command without a running emulator
- **WHEN** the user runs an offline subcommand (e.g. `lstk cdk synth`, `lstk cdk ls`, `lstk cdk init`)
- **THEN** lstk runs it without requiring a running emulator

### Requirement: Streamed passthrough output
The system SHALL stream the CDK subprocess's stdin, stdout, and stderr through unobstructed and SHALL NOT display a spinner or capture CDK output into lifecycle events.

#### Scenario: Unobstructed streaming
- **WHEN** a long-running CDK command (e.g. `lstk cdk deploy`) produces incremental output
- **THEN** lstk streams that output directly to the terminal without a spinner or reformatting, and forwards interactive prompts via stdin

#### Scenario: Propagate failure
- **WHEN** the CDK command exits non-zero
- **THEN** lstk returns a silent error carrying that exit status so the top-level handler does not reprint it
