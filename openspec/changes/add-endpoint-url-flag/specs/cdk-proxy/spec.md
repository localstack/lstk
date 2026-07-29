## MODIFIED Requirements

### Requirement: CDK CLI proxy command

The system SHALL provide an `lstk cdk` command that forwards all of its arguments to the real AWS CDK CLI (`cdk`) and, before invoking it, configures the subprocess environment to target the running LocalStack instance.

#### Scenario: Pass through CDK arguments

- **WHEN** the user runs `lstk cdk deploy MyStack --require-approval never`
- **THEN** lstk invokes the `cdk` binary with `deploy MyStack --require-approval never` intact and propagates its exit code

#### Scenario: Inject LocalStack endpoint into the CDK environment

- **WHEN** lstk runs a CDK command
- **THEN** the `cdk` subprocess receives `AWS_ENDPOINT_URL` set to the resolved LocalStack endpoint and `AWS_ENDPOINT_URL_S3` set to the corresponding S3 endpoint (with an `s3.` host prefix when the host is virtual-host-capable)

#### Scenario: Honor an explicit endpoint override via AWS_ENDPOINT_URL

- **WHEN** `AWS_ENDPOINT_URL` is already set in the environment and neither `--endpoint-url` nor `LSTK_ENDPOINT_URL` is set
- **THEN** lstk uses that value instead of the auto-resolved endpoint
- **AND** lstk does not perform Docker container discovery, instead verifying the given endpoint is reachable and is an AWS LocalStack emulator via HTTP probing

#### Scenario: --endpoint-url and LSTK_ENDPOINT_URL take precedence over AWS_ENDPOINT_URL

- **WHEN** `AWS_ENDPOINT_URL` is set together with the global `--endpoint-url` flag or `LSTK_ENDPOINT_URL`
- **THEN** the higher-precedence source's value is used for the endpoint, and `AWS_ENDPOINT_URL` has no effect

### Requirement: Emulator gating for AWS-contacting commands

The system SHALL require a running AWS emulator for CDK subcommands that contact AWS APIs and SHALL run a fixed set of offline subcommands without that requirement.

When a global endpoint URL is resolved (via `--endpoint-url`, `LSTK_ENDPOINT_URL`, or `AWS_ENDPOINT_URL`), AWS-contacting subcommands SHALL skip Docker-based container discovery and instead verify that URL is reachable and is an AWS emulator via HTTP probing.

#### Scenario: AWS-contacting command without a running emulator

- **WHEN** the user runs an AWS-contacting subcommand (e.g. `lstk cdk deploy`) and the AWS emulator is not running
- **THEN** lstk emits an actionable "LocalStack is not running" error (with a command to start it) and does not invoke `cdk`

#### Scenario: A different emulator is running

- **WHEN** an AWS-contacting CDK command is run while a non-AWS emulator (e.g. Snowflake or Azure) is running but the AWS emulator is not
- **THEN** lstk fails with an AWS-specific error naming the running emulator rather than a misleading generic "not running" message

#### Scenario: Offline command without a running emulator

- **WHEN** the user runs an offline subcommand (e.g. `lstk cdk synth`, `lstk cdk ls`, `lstk cdk init`)
- **THEN** lstk runs it without requiring a running emulator

#### Scenario: Target an externally-managed emulator via --endpoint-url

- **WHEN** the user runs an AWS-contacting subcommand (e.g. `lstk cdk deploy --endpoint-url http://localhost:4566`) and no local Docker container is running
- **THEN** lstk skips Docker container discovery, probes the given URL to confirm it is reachable and is an AWS LocalStack emulator, and injects it as `AWS_ENDPOINT_URL`/`AWS_ENDPOINT_URL_S3` for the `cdk` subprocess

#### Scenario: Externally-managed endpoint is not AWS

- **WHEN** a global endpoint URL is resolved for an AWS-contacting CDK command and the endpoint is detected as a non-AWS emulator
- **THEN** lstk fails with the same AWS-specific error as the non-AWS-emulator-running scenario, naming the detected type, and does not invoke `cdk`
