## MODIFIED Requirements

### Requirement: SAM CLI proxy command

The system SHALL provide an `lstk sam` command that forwards all of its arguments to the real AWS SAM CLI (`sam`) and, before invoking it, configures the subprocess environment to target the running LocalStack instance.

#### Scenario: Pass through SAM arguments

- **WHEN** the user runs `lstk sam deploy --stack-name my-stack --no-confirm-changeset`
- **THEN** lstk invokes the `sam` binary with `deploy --stack-name my-stack --no-confirm-changeset` intact and propagates its exit code

#### Scenario: Inject LocalStack endpoint into the SAM environment

- **WHEN** lstk runs a SAM command
- **THEN** the `sam` subprocess receives `AWS_ENDPOINT_URL` set to the resolved LocalStack endpoint, and lstk does not set `AWS_ENDPOINT_URL_S3` or any S3 path-style configuration (SAM's botocore auto-selects path-style addressing against a `localhost`/IP endpoint)

#### Scenario: Honor an explicit endpoint override via AWS_ENDPOINT_URL

- **WHEN** `AWS_ENDPOINT_URL` is already set in the environment and neither `--endpoint-url` nor `LSTK_ENDPOINT_URL` is set
- **THEN** lstk uses that value instead of the auto-resolved endpoint
- **AND** lstk does not perform Docker container discovery, instead verifying the given endpoint is reachable and is an AWS LocalStack emulator via HTTP probing

#### Scenario: --endpoint-url and LSTK_ENDPOINT_URL take precedence over AWS_ENDPOINT_URL

- **WHEN** `AWS_ENDPOINT_URL` is set together with the global `--endpoint-url` flag or `LSTK_ENDPOINT_URL`
- **THEN** the higher-precedence source's value is used for the endpoint, and `AWS_ENDPOINT_URL` has no effect

#### Scenario: Honor an explicit S3 endpoint override

- **WHEN** `AWS_ENDPOINT_URL_S3` is already set in the environment
- **THEN** lstk passes it through to the `sam` subprocess unchanged (it is neither set nor stripped by lstk), so a user can override S3 addressing for an exotic case

### Requirement: Emulator gating for AWS-contacting commands

The system SHALL require a running AWS emulator for SAM subcommands that contact AWS APIs and SHALL run a fixed set of offline subcommands without that requirement.

When a global endpoint URL is resolved (via `--endpoint-url`, `LSTK_ENDPOINT_URL`, or `AWS_ENDPOINT_URL`), AWS-contacting subcommands SHALL skip Docker-based container discovery and instead verify that URL is reachable and is an AWS emulator via HTTP probing.

#### Scenario: AWS-contacting command without a running emulator

- **WHEN** the user runs an AWS-contacting subcommand (e.g. `lstk sam deploy`) and the AWS emulator is not running
- **THEN** lstk emits an actionable "LocalStack is not running" error (with a command to start it) and does not invoke `sam`

#### Scenario: A different emulator is running

- **WHEN** an AWS-contacting SAM command is run while a non-AWS emulator (e.g. Snowflake or Azure) is running but the AWS emulator is not
- **THEN** lstk fails with an AWS-specific error naming the running emulator rather than a misleading generic "not running" message

#### Scenario: Offline command without a running emulator

- **WHEN** the user runs an offline subcommand (e.g. `lstk sam init`, `lstk sam build`, `lstk sam validate`, `lstk sam local generate-event`)
- **THEN** lstk runs it without requiring a running emulator

#### Scenario: Target an externally-managed emulator via --endpoint-url

- **WHEN** the user runs an AWS-contacting subcommand (e.g. `lstk sam deploy --endpoint-url http://localhost:4566`) and no local Docker container is running
- **THEN** lstk skips Docker container discovery, probes the given URL to confirm it is reachable and is an AWS LocalStack emulator, and uses it for the `sam` subprocess's AWS environment

#### Scenario: Externally-managed endpoint is not AWS

- **WHEN** a global endpoint URL is resolved for an AWS-contacting SAM command and the endpoint is detected as a non-AWS emulator
- **THEN** lstk fails with the same AWS-specific error as the non-AWS-emulator-running scenario, naming the detected type, and does not invoke `sam`
