# external-endpoint Specification

## Purpose

Let lstk target a LocalStack emulator it did not start — docker compose, host-network mode, CI, a different machine, or a cloud-hosted ephemeral instance — via a global `--endpoint-url` flag (and the `LSTK_ENDPOINT_URL`/`AWS_ENDPOINT_URL` environment variables) instead of discovering one through local Docker inspection, defining which commands honor it, which reject it, and which ignore it.

## Requirements

### Requirement: Global endpoint URL flag and precedence

The system SHALL provide a global `--endpoint-url <url>` persistent flag and an `LSTK_ENDPOINT_URL` environment variable that let `aws`, `az`, `terraform`/`tf`, `cdk`, `sam`, `snapshot save`/`load` (including the `lstk save`/`lstk load` aliases), `snapshot remove`, `snapshot list` (when given an `s3://...` argument), `reset`, and `status` target an emulator at an arbitrary URL instead of one discovered via local Docker inspection. The value SHALL include a scheme — `http` or `https` (e.g. `http://host:4566` or `https://host`) — and SHALL be validated as a URL at the command boundary; any other scheme is rejected. The resolved scheme SHALL be preserved unchanged all the way to whatever ultimately makes the request (the wrapped `aws`/`terraform`/`cdk`/`sam` tools, and the emulator API calls behind `snapshot`/`reset`/`status`), never normalized to `http`. Certificate trust for `https` endpoints follows standard TLS verification; there is no option to skip certificate verification.

For `aws`, `az`, `terraform`/`tf`, `cdk`, and `sam` (which forward unrecognized arguments to a wrapped binary and disable lstk's own flag parsing for their args), the `--endpoint-url` flag SHALL be recognized only when it precedes the subcommand name (e.g. `lstk --endpoint-url http://localhost:4566 aws s3 ls`), the same placement rule already applied to `--json` for these five commands. This is required because the `aws` CLI itself has a native `--endpoint-url` flag: placed after the subcommand name (`lstk aws --endpoint-url http://localhost:4566 s3 ls`), it SHALL be forwarded to the wrapped binary unchanged rather than intercepted by lstk, so a user's own `aws --endpoint-url` usage keeps working identically under `lstk aws`. The other four (`az`, `terraform`/`tf`, `cdk`, `sam`) have no such collision but get the identical pre-command-only rule for consistency. `snapshot`, `reset`, and `status` have ordinary Cobra flag parsing and accept `--endpoint-url` in the normal position after the command name.

#### Scenario: Flag targets an arbitrary endpoint

- **WHEN** a user runs `lstk --endpoint-url http://localhost:4566 aws s3 ls`
- **THEN** lstk uses `http://localhost:4566` as the emulator endpoint without performing Docker container discovery

#### Scenario: Post-command --endpoint-url on aws passes through untouched

- **WHEN** a user runs `lstk aws --endpoint-url http://localhost:4566 s3 ls`
- **THEN** lstk does not treat `--endpoint-url` as its own flag — it forwards `--endpoint-url http://localhost:4566 s3 ls` to the `aws` binary unchanged, and resolves its own emulator endpoint via existing Docker discovery (or another endpoint source) as if no `--endpoint-url` were given

#### Scenario: Environment variable is honored

- **WHEN** `LSTK_ENDPOINT_URL` is set and no `--endpoint-url` flag is passed
- **THEN** lstk resolves the endpoint from the environment variable

#### Scenario: AWS_ENDPOINT_URL is honored as a backup for every in-scope command

- **WHEN** `AWS_ENDPOINT_URL` is set, and neither `--endpoint-url` nor `LSTK_ENDPOINT_URL` is set, and the user runs `lstk aws s3 ls`, `lstk az ...`, `lstk status`, `lstk reset`, or an in-scope `lstk snapshot` form
- **THEN** lstk resolves the endpoint from `AWS_ENDPOINT_URL` without performing Docker container discovery, in every one of those cases

#### Scenario: AWS_ENDPOINT_URL pointing at a non-LocalStack endpoint fails closed

- **WHEN** `AWS_ENDPOINT_URL` is set to a real AWS endpoint or an unrelated service, and neither `--endpoint-url` nor `LSTK_ENDPOINT_URL` is set, and the user runs `lstk status`
- **THEN** the reachability/type probe rejects it with a "doesn't look like a LocalStack emulator" error rather than silently proceeding against it

#### Scenario: Flag takes precedence over environment sources

- **WHEN** `--endpoint-url` is passed and `LSTK_ENDPOINT_URL` and/or `AWS_ENDPOINT_URL` are also set
- **THEN** the flag's value is used

#### Scenario: LSTK_ENDPOINT_URL takes precedence over AWS_ENDPOINT_URL

- **WHEN** both `LSTK_ENDPOINT_URL` and `AWS_ENDPOINT_URL` are set, and no `--endpoint-url` flag is passed
- **THEN** `LSTK_ENDPOINT_URL`'s value is used

#### Scenario: Malformed endpoint URL

- **WHEN** `--endpoint-url` (or `LSTK_ENDPOINT_URL`/`AWS_ENDPOINT_URL`, where applicable) is set to a value that is not a valid absolute URL with a scheme
- **THEN** the command fails immediately with an actionable error and does not attempt any network call

#### Scenario: An unsupported scheme is rejected

- **WHEN** `--endpoint-url` is set to a value with a scheme other than `http` or `https` (e.g. `ftp://host:4566`)
- **THEN** the command fails immediately with an actionable error naming the unsupported scheme and does not attempt any network call

#### Scenario: https endpoint is accepted and used as-is

- **WHEN** a user runs `lstk --endpoint-url https://my-instance.ephemeral-instances.localstack.cloud aws s3 ls`
- **THEN** lstk probes and targets that `https://` URL directly, with no local Docker involvement, and the `aws` binary receives `https://my-instance.ephemeral-instances.localstack.cloud` as its endpoint — not a value rewritten to `http://`

#### Scenario: https endpoint reaches the emulator API layer unchanged

- **WHEN** a user runs `lstk snapshot save --endpoint-url https://my-instance.ephemeral-instances.localstack.cloud my-baseline`
- **THEN** the save request is made against the given `https://` endpoint, not downgraded to `http://`

### Requirement: Emulator type detection for externally-managed endpoints

When an endpoint URL is resolved for a command that needs to know the emulator type (e.g. to enforce an AWS-only requirement, or to render it in `status`), the system SHALL determine the type by probing the endpoint's `/_localstack/health` (falling back to `/_localstack/info` when the health response lacks a `version` field) rather than reading a local config `type`. There SHALL be no manual override flag or config setting for the type — detection is the only mechanism.

#### Scenario: Type detected from health probe

- **WHEN** an endpoint URL is resolved
- **THEN** lstk determines the emulator type by inspecting the endpoint's health/info response before proceeding

#### Scenario: Detection is inconclusive

- **WHEN** the endpoint's health/info response cannot be classified as aws, azure, or snowflake
- **THEN** the command fails with an actionable error stating that the emulator type could not be determined from the endpoint's health response, without suggesting a flag or setting to force a type

### Requirement: Docker preflight is skipped for externally-managed endpoints

When an endpoint URL is resolved for `aws`, `az`, `terraform`/`tf`, `cdk`, `sam`, `snapshot save`/`load`, `snapshot remove`, `snapshot list s3://...`, `reset`, or `status`, the system SHALL NOT construct a Docker runtime, check Docker health, or look up a running container by name or image. Instead it SHALL verify the given endpoint is reachable and responds with a recognizable LocalStack health payload before proceeding.

#### Scenario: No Docker required

- **WHEN** a user runs `lstk status --endpoint-url http://localhost:4566` on a machine with no Docker daemon running
- **THEN** the command does not fail due to Docker being unavailable, and instead probes the given URL directly

#### Scenario: Unreachable externally-managed endpoint

- **WHEN** an endpoint URL is resolved and the endpoint does not respond, or responds with something that isn't a recognizable LocalStack health payload
- **THEN** the command fails with an actionable error naming the given URL and the failure cause, and does not suggest running `lstk start` or mention Docker

#### Scenario: The same endpoint responds under the other scheme

- **WHEN** an endpoint URL is unreachable, but the same host and port respond as a LocalStack emulator under the other scheme — e.g. `http://<instance>.localstack.cloud` given for a TLS-terminated cloud-hosted instance, where the raw failure is only "no route to host" against port 80
- **THEN** the error additionally names the URL under the scheme that did respond and tells the user to retry with it, and lstk does not silently substitute that scheme for the one the user gave

### Requirement: snapshot load does not auto-start a container for externally-managed endpoints

`lstk snapshot load` (and its `lstk load` alias) SHALL NOT fall back to auto-starting a local Docker container when an endpoint URL is resolved, even if that endpoint is currently unreachable.

#### Scenario: Auto-start suppressed

- **WHEN** a user runs `lstk snapshot load some.snapshot --endpoint-url http://localhost:4566` and nothing responds at that URL
- **THEN** lstk fails with the unreachable-endpoint error rather than starting a local Docker container

### Requirement: status reports reduced detail for externally-managed endpoints

When `lstk status` resolves an endpoint URL instead of a Docker-managed container, it SHALL report reachability, the detected emulator type and version reported by the endpoint's own health payload, and — for an AWS-typed target — deployed resources exactly as it does for a Docker-managed emulator: deployed resources are reported via the emulator's own `/_localstack/resources` API, not derived from Docker, so there is no reason to omit them. It SHALL NOT report Docker-derived facts (container uptime, image, bound port) that don't exist for an emulator lstk didn't start.

Targeting an external endpoint changes which facts are available, not how they are rendered: `status` SHALL select its output mode the same way as the Docker-managed path — the interactive TUI on a terminal, the plain sink otherwise — so styling and spacing are identical between the two paths.

#### Scenario: Status for an externally-managed endpoint

- **WHEN** a user runs `lstk status --endpoint-url http://localhost:4566` against a reachable emulator
- **THEN** the output shows the endpoint, detected type, and reported version, without container uptime/image fields

#### Scenario: Status for an externally-managed AWS endpoint reports deployed resources

- **WHEN** a user runs `lstk status --endpoint-url http://localhost:4566` against a reachable AWS-typed emulator with deployed resources
- **THEN** the output includes the resource summary and table, the same as it would for a Docker-managed AWS emulator

#### Scenario: Status for an externally-managed endpoint renders through the TUI on a terminal

- **WHEN** a user runs `lstk status --endpoint-url http://localhost:4566` attached to a terminal
- **THEN** the output is rendered by the interactive TUI, with the same styling and spacing a Docker-managed `lstk status` produces

### Requirement: Docker-lifecycle and filesystem commands reject any resolved endpoint URL source

`lstk logs`, `lstk stop`, `lstk restart`, `lstk volume`, and `lstk start` SHALL reject with an actionable error whenever an endpoint URL is resolved for the invocation — whether from an explicit `--endpoint-url` flag, `LSTK_ENDPOINT_URL`, or `AWS_ENDPOINT_URL` — explaining that the command operates on a local Docker container or local filesystem state with no remote equivalent. This SHALL apply even when a local emulator is currently running and reachable: proceeding against it while an endpoint source signals the user's actual target is elsewhere is a wrong-target risk (stopping, restarting, streaming logs from, clearing the volume of, or starting a redundant instance of the wrong emulator), not a harmless no-op — and that risk is identical whether the endpoint source is an explicit flag on this command line or an environment variable set earlier in the session. lstk SHALL NOT check whether a local emulator is running before rejecting.

#### Scenario: Explicit flag on an excluded command is rejected

- **WHEN** a user runs `lstk logs --endpoint-url http://localhost:4566`
- **THEN** lstk fails immediately with an actionable error stating `logs` does not support `--endpoint-url`, and does not read any container logs

#### Scenario: start rejects an explicit endpoint URL

- **WHEN** a user runs `lstk start --endpoint-url http://localhost:4566`
- **THEN** lstk fails immediately with an actionable error stating `start` does not support `--endpoint-url`, and does not attempt to start any emulator

#### Scenario: Ambient environment variable is rejected too, even with no explicit flag

- **WHEN** `LSTK_ENDPOINT_URL` (or `AWS_ENDPOINT_URL`) is set in the environment and the user runs `lstk stop` with no `--endpoint-url` flag
- **THEN** lstk fails immediately with the same actionable error as the explicit-flag case, naming the environment variable that triggered it

#### Scenario: Rejection happens even when a local emulator is running

- **WHEN** `LSTK_ENDPOINT_URL` is set in the environment, a local Docker-managed emulator is currently running and reachable, and the user runs `lstk restart` with no `--endpoint-url` flag
- **THEN** lstk still rejects the command with the actionable error rather than restarting the local emulator, without ever checking whether a local emulator is running

### Requirement: Platform-only snapshot forms silently ignore endpoint URL sources

`lstk snapshot show` and `lstk snapshot list` when given no `s3://` argument SHALL silently ignore `--endpoint-url`, `LSTK_ENDPOINT_URL`, and `AWS_ENDPOINT_URL` (whether set via flag or environment) and proceed exactly as they would without any endpoint URL resolved. Both forms only ever query the LocalStack platform API, which is account-scoped rather than emulator-scoped, so no endpoint source could change their result — unlike the Docker-lifecycle commands, ignoring these sources carries no wrong-target risk, so an explicit flag is accepted without error rather than rejected.

#### Scenario: Explicit flag on snapshot show has no effect

- **WHEN** a user runs `lstk snapshot show pod:my-baseline --endpoint-url http://localhost:4566`
- **THEN** lstk shows the pod's metadata exactly as it would without the flag, and does not fail or attempt to reach the given URL

#### Scenario: Explicit flag on bare snapshot list has no effect

- **WHEN** a user runs `lstk snapshot list --endpoint-url http://localhost:4566` with no `s3://` argument
- **THEN** lstk lists the user's cloud snapshots exactly as it would without the flag, and does not fail or attempt to reach the given URL
