## ADDED Requirements

### Requirement: Enumerated, stable error codes
Every `error.code` value emitted in a JSON envelope SHALL be one of a fixed, documented set of `SCREAMING_SNAKE_CASE` string constants. lstk SHALL NOT emit a free-text or ad hoc string as `error.code`; a code not yet covering some failure mode SHALL fall back to `INTERNAL_ERROR` rather than inventing an undocumented one at the call site. The full set, as of this change:

| Code | Meaning | Retryable |
|---|---|---|
| `RUNTIME_UNAVAILABLE` | The container runtime (`internal/runtime.Runtime` — Docker today; Podman, Rancher Desktop, Finch, or Kubernetes are architecturally anticipated but not yet implemented) is unreachable or unhealthy | Yes |
| `IMAGE_PULL_FAILED` | Pulling the emulator image failed and no usable local image exists | Yes |
| `EMULATOR_NOT_RUNNING` | The targeted emulator is not currently running | No |
| `EMULATOR_ALREADY_RUNNING` | An emulator is already running where the command expected it not to be | No |
| `EMULATOR_WRONG_TYPE` | The command requires a specific emulator type but a different one is configured/running | No |
| `EMULATOR_NOT_CONFIGURED` | No container of the requested type exists in the resolved config | No |
| `EMULATOR_START_FAILED` | The emulator failed to reach a healthy state after starting | Yes |
| `AUTH_REQUIRED` | The operation needs a LocalStack auth token and none is available | No |
| `AUTH_LOGIN_FAILED` | An authentication flow failed | Yes |
| `CREDENTIALS_MISSING` | Required third-party credentials (e.g. AWS credentials for an S3 remote) could not be resolved | No |
| `LICENSE_INVALID` | The platform rejected the configured license/token | No |
| `LICENSE_UNSUPPORTED_TAG` | The configured image tag is not covered by the license | No |
| `SNAPSHOT_NOT_FOUND` | The referenced snapshot does not exist | No |
| `SNAPSHOT_INVALID_REF` | The snapshot reference could not be parsed | No |
| `SNAPSHOT_REMOTE_ERROR` | A platform or S3 remote call failed | Yes |
| `SNAPSHOT_BUCKET_NOT_FOUND` | The pre-flight S3 bucket-existence check failed | No |
| `CONFIG_INVALID` | The config file failed to parse or validate | No |
| `CONFIG_NOT_FOUND` | An explicit config path does not exist | No |
| `INTEGRATION_NOT_SET_UP` | A required one-time setup step (e.g. `lstk setup azure`) has not been run | No |
| `DEPENDENCY_MISSING` | A required external CLI (e.g. `az`) is not on `PATH` | No |
| `DNS_RESOLUTION_REQUIRED` | A required hostname pattern does not resolve | No |
| `CONFIRMATION_REQUIRED` | A destructive action needs `--force` outside an interactive terminal | No |
| `VALIDATION_ERROR` | A semantically invalid combination of flags/arguments was given | No |
| `USAGE_ERROR` | Cobra-level flag or argument parsing failed | No |
| `NOT_JSON_CAPABLE` | The requested command has not been annotated as JSON-capable | No |
| `NETWORK_ERROR` | An unclassified network/transport failure occurred | Yes |
| `CANCELLED` | The operation was interrupted (e.g. context cancellation via Ctrl+C) | Yes |
| `INTERNAL_ERROR` | Unclassified or unexpected failure; the universal fallback | No |

#### Scenario: Error code is one of the documented constants
- **WHEN** any JSON-capable command emits an `error` object
- **THEN** `error.code` is exactly one of the documented string constants above

#### Scenario: Unmapped failure falls back to INTERNAL_ERROR
- **WHEN** a JSON-capable command hits a failure that has not been mapped to a specific code
- **THEN** `error.code` is `"INTERNAL_ERROR"`, not an invented or free-text value

### Requirement: Error objects carry a human message alongside the code
An `error` object SHALL include a `message` string suitable for human display, in addition to `code`. Scripts SHALL treat `message` as informational only and SHALL branch on `code`, since `message` text is not guaranteed to remain stable across versions.

#### Scenario: Error includes both code and message
- **WHEN** a JSON-capable command fails
- **THEN** the emitted `error` object has both a `code` field (stable) and a `message` field (human-readable, not guaranteed stable)

### Requirement: Error objects declare whether the failure is retryable
Every `error` object SHALL include a `retryable` boolean, a static property of `code` (the same code always carries the same value, per the table above) rather than something computed per failure instance. `retryable: true` SHALL mean the identical invocation might succeed later without any change to arguments or environment (e.g. a transient runtime/network hiccup); `retryable: false` SHALL mean the invocation will keep failing the same way until something about the request, config, or environment changes (e.g. a missing `--force`, an invalid reference, a validation error).

#### Scenario: A transient failure is marked retryable
- **WHEN** a JSON-capable command fails with `error.code: "RUNTIME_UNAVAILABLE"`
- **THEN** the error object has `"retryable": true`

#### Scenario: A failure requiring a different invocation is not marked retryable
- **WHEN** a JSON-capable command fails with `error.code: "VALIDATION_ERROR"` or `"CONFIRMATION_REQUIRED"`
- **THEN** the error object has `"retryable": false`

#### Scenario: retryable is consistent for a given code
- **WHEN** the same `error.code` is emitted by two different commands
- **THEN** both error objects report the same `retryable` value for that code

### Requirement: Error actions are machine-usable
When an error has a suggested remediation (mirroring the plain-text `ErrorAction` used in interactive/plain rendering today), the JSON error object's `actions` array SHALL contain objects with a stable `id` slug and a literal `command` string, rather than a pre-formatted display line.

#### Scenario: Remediation is structured, not pre-formatted text
- **WHEN** an error such as `EMULATOR_NOT_RUNNING` has a suggested next step
- **THEN** `error.actions` contains at least one object of the form `{"id": "<slug>", "command": "<literal shell command>"}`, not a formatted string like `"==> Start LocalStack: lstk"`
