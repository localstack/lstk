## MODIFIED Requirements

### Requirement: LocalStack must be running

The command SHALL require a running LocalStack **AWS** emulator. By default it SHALL resolve the endpoint automatically using lstk's container discovery and host resolution, without requiring the user to specify a host or port. `lstk terraform` operates only against the AWS emulator; other emulator types (e.g. Snowflake, Azure) are not supported.

When a global endpoint URL is resolved (via `--endpoint-url`, `LSTK_ENDPOINT_URL`, or `AWS_ENDPOINT_URL` — see precedence below), the command SHALL skip Docker-based container discovery entirely and instead treat that URL as the emulator endpoint, verifying it is reachable and determining its emulator type via HTTP probing instead of container inspection. The AWS-only requirement still applies in this mode.

`AWS_ENDPOINT_URL` SHALL be honored as the lowest-precedence of the three endpoint sources: `--endpoint-url`, then `LSTK_ENDPOINT_URL`, then `AWS_ENDPOINT_URL`. When `AWS_ENDPOINT_URL` is the only one of the three set, its value is used as the endpoint **and** it now triggers the same Docker-bypass behavior as the other two sources — previously (before this capability existed) `AWS_ENDPOINT_URL` only relabeled the auto-resolved endpoint's value while the Docker running-check still applied unconditionally; this is a breaking change to that narrower behavior.

#### Scenario: No running emulator

- **WHEN** a user runs `lstk terraform plan` and no LocalStack AWS emulator is running
- **THEN** the command fails with an error stating LocalStack is not running and suggesting how to start it (`lstk`)
- **AND** the `terraform` binary is not invoked

#### Scenario: A non-AWS emulator is running

- **WHEN** a user runs `lstk terraform plan` while a non-AWS LocalStack emulator (e.g. Snowflake or Azure) is running but the AWS emulator is not
- **THEN** the command fails with an error that specifically states `lstk terraform` requires the AWS emulator and identifies the running emulator type
- **AND** the `terraform` binary is not invoked

#### Scenario: Endpoint resolved from running emulator

- **WHEN** a LocalStack AWS emulator is running and no global endpoint URL is resolved
- **THEN** the command resolves the endpoint via the same discovery used by `lstk aws` (container discovery plus host resolution)
- **AND** uses that endpoint as the base for all generated provider endpoints

#### Scenario: Explicit endpoint override via AWS_ENDPOINT_URL

- **WHEN** the `AWS_ENDPOINT_URL` environment variable is set and neither `--endpoint-url` nor `LSTK_ENDPOINT_URL` is set
- **THEN** its host and port take precedence over the auto-resolved endpoint when building the provider override
- **AND** the command does not perform Docker container discovery, instead verifying the given endpoint is reachable and is an AWS LocalStack emulator via HTTP probing

#### Scenario: --endpoint-url and LSTK_ENDPOINT_URL take precedence over AWS_ENDPOINT_URL

- **WHEN** `AWS_ENDPOINT_URL` is set together with `--endpoint-url` or `LSTK_ENDPOINT_URL`
- **THEN** the higher-precedence source's value is used for the endpoint, and `AWS_ENDPOINT_URL` has no effect

#### Scenario: Target an externally-managed emulator via --endpoint-url or LSTK_ENDPOINT_URL

- **WHEN** a user runs `lstk terraform plan --endpoint-url http://localhost:4566` (or sets `LSTK_ENDPOINT_URL`) and no local Docker container is running
- **THEN** the command does not perform Docker container discovery, probes the given URL to confirm it is reachable and is an AWS LocalStack emulator, and uses it as the base for all generated provider endpoints

#### Scenario: Externally-managed endpoint is not AWS

- **WHEN** a global endpoint URL is resolved and the endpoint is detected as a non-AWS emulator
- **THEN** the command fails with the same AWS-specific error as the non-AWS-emulator-running scenario, naming the detected type, and does not invoke `terraform`

#### Scenario: Externally-managed endpoint is unreachable

- **WHEN** a global endpoint URL is resolved and the endpoint does not respond, or its response doesn't look like a LocalStack health payload
- **THEN** the command fails with an actionable error naming the given URL and the cause, without suggesting `lstk start` or mentioning Docker
