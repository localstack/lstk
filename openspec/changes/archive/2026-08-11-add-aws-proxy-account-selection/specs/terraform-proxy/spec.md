## MODIFIED Requirements

### Requirement: State bucket and lock table provisioning

Before initializing an S3 backend, the system SHALL ensure the backend's resources exist in LocalStack, because a fresh LocalStack instance contains no state bucket. The system SHALL create the configured state `bucket` if it does not already exist, honoring the backend's configured region. When the backend configures DynamoDB-based locking via `dynamodb_table`, the system SHALL create that table if it does not already exist, with the lock schema Terraform expects (hash key `LockID`). When the backend uses S3-native locking (`use_lockfile = true`) or configures no locking, the system SHALL NOT create a DynamoDB table.

Provisioning SHALL be idempotent: an already-existing bucket or table SHALL be treated as success, not an error. Provisioning SHALL target the resolved LocalStack endpoint and SHALL occur before `terraform` is invoked.

Provisioning SHALL use the same resolved account as the generated backend configuration (see "Region and account selection"): the `AWS_ACCESS_KEY_ID` of the provisioning calls SHALL be set to that account, overriding any ambient value, and the secret SHALL remain the mock value. LocalStack partitions resources by account, so a bucket provisioned under a different account than the backend block addresses is invisible to `terraform init`, which would then fail against a bucket lstk had just reported creating.

#### Scenario: State bucket is created when absent

- **WHEN** an S3 backend names a bucket that does not yet exist in LocalStack and the user runs `lstk terraform init`
- **THEN** the system creates that bucket in LocalStack before invoking `terraform`
- **AND** `terraform init` succeeds against the now-existing bucket

#### Scenario: Existing bucket is not an error

- **WHEN** the configured state bucket already exists in LocalStack
- **THEN** provisioning treats it as success and proceeds without error

#### Scenario: DynamoDB lock table is created only when configured

- **WHEN** the S3 backend sets `dynamodb_table` and that table does not exist
- **THEN** the system creates the table with hash key `LockID` before invoking `terraform`

#### Scenario: No lock table for S3-native or lock-free backends

- **WHEN** the S3 backend sets `use_lockfile = true` or configures no DynamoDB locking
- **THEN** the system does not create a DynamoDB table

#### Scenario: Provisioning targets the selected account

- **WHEN** the user runs `lstk terraform --account 111111111111 init` against a configuration declaring an S3 backend
- **THEN** the state bucket (and lock table, when configured) is provisioned in LocalStack account `111111111111`, the same account the generated backend block's `access_key` addresses
- **AND** `terraform init` finds the bucket

#### Scenario: Provisioning ignores an ambient access key

- **WHEN** the environment sets `AWS_ACCESS_KEY_ID` and provisioning runs for an S3 backend
- **THEN** the provisioning calls use the resolved account rather than the ambient value verbatim, so they cannot address a different account than the generated backend block

### Requirement: Region and account selection

The command SHALL accept two lstk-specific flags, `--region` and `--account`, that set the deployment region and the target AWS account ID for the generated provider override. Because these are not standard `terraform` flags, the command SHALL parse and remove them (together with their values) from the argument list before forwarding the remaining arguments to the `terraform` binary. Both `--flag value` and `--flag=value` forms SHALL be supported.

These flags SHALL be recognized only in leading position — that is, after `terraform`/`tf` and before the terraform action. The leading run ends at the action, **not** at the first argument lstk does not recognize: an argument belonging to the wrapped tool SHALL be forwarded unchanged and scanning SHALL continue, so lstk's flags are recognized in any order relative to the tool's own. Any `--region`/`--account` appearing after the action SHALL be treated as ordinary arguments for the wrapped tool and forwarded unchanged. The flags SHALL NOT be defined as root/persistent flags, so a flag placed before the `terraform` subcommand (e.g. `lstk --account … terraform`) is not accepted.

Locating the action without knowledge of every wrapped tool's own flags SHALL rest on a single rule: a bare argument following a flag that may still take a value (one containing no `=`) is that flag's value. At most one bare argument SHALL be absorbed per flag, so scanning always halts at or before the second consecutive bare argument, which bounds it to the wrapped tool's action at the latest. That bound is what prevents lstk from consuming an `--account` that genuinely belongs to the wrapped tool — see the `aws-proxy` capability, where the AWS CLI defines a real `--account` parameter on several operations.

This paragraph is the normative statement of leading-flag parsing for every proxy; `cdk-proxy`, `sam-proxy`, and `aws-proxy` refer to it rather than restating it.

The resolved region SHALL be selected with precedence: `--region` flag, then the `AWS_REGION` environment variable, then a default of `us-east-1`. The deprecated `AWS_DEFAULT_REGION` environment variable SHALL NOT be consulted. The system SHALL additionally record whether the region was named by the flag rather than inherited or defaulted, because `sam-proxy` needs that distinction (see its "Region selection" requirement).

The resolved account (provider `access_key`) SHALL be selected with precedence: `--account` flag, then the `AWS_ACCESS_KEY_ID` environment variable, then a default of `test`.

The `--account` flag value SHALL be validated to be exactly 12 digits (`^\d{12}$`). A value supplied via `AWS_ACCESS_KEY_ID` SHALL NOT be validated, but SHALL be passed through an access-key deactivation step: if it begins with the letter `A` (the prefix of real AWS access key ids such as `AKIA…` long-term keys and `ASIA…` temporary session keys), the leading `A` SHALL be replaced with `L` before the value is encoded into the override. This guards against a real AWS credential accidentally present in the environment being written into the generated override file or sent to LocalStack. The validated 12-digit `--account` flag value is used as-is (it cannot begin with `A`).

For unproxied subcommands (`fmt`, `validate`, `version`), both flags SHALL be a no-op: they are stripped from the arguments, not forwarded to `terraform`, and have no other effect.

- **WHEN** a user runs `lstk terraform --region us-west-2 plan`
- **THEN** `--region us-west-2` is removed from the forwarded arguments (leaving `plan`)
- **AND** the generated provider blocks set `region = "us-west-2"`

#### Scenario: Region flag encoded into override

- **WHEN** a user runs `lstk terraform --region us-west-2 plan`
- **THEN** `--region us-west-2` is removed from the forwarded arguments (leaving `plan`)
- **AND** the generated provider blocks set `region = "us-west-2"`

#### Scenario: Account flag encoded into override

- **WHEN** a user runs `lstk terraform --account 111111111111 apply`
- **THEN** `--account 111111111111` is removed from the forwarded arguments (leaving `apply`)
- **AND** the generated provider blocks set `access_key = "111111111111"`

#### Scenario: Flags must lead — placement after the action is not recognized

- **WHEN** a user runs `lstk terraform plan --region us-west-2`
- **THEN** parsing stops at `plan`, so `--region us-west-2` is NOT consumed by lstk
- **AND** `--region us-west-2` is forwarded to `terraform` as-is (where terraform rejects it as an unknown flag)

#### Scenario: Flags before the terraform subcommand are rejected

- **WHEN** a user runs `lstk --account 111111111111 terraform plan`
- **THEN** the command fails with an error stating that `--region`/`--account` must appear after the terraform subcommand
- **AND** the `terraform` binary is not invoked

#### Scenario: Region falls back to environment then default

- **WHEN** `--region` is not supplied
- **THEN** the region is taken from `AWS_REGION` if set, otherwise `us-east-1`

#### Scenario: Account falls back to environment then default

- **WHEN** `--account` is not supplied
- **THEN** the `access_key` is taken from `AWS_ACCESS_KEY_ID` if set, otherwise `test`

#### Scenario: Real AWS access key from the environment is deactivated

- **WHEN** `--account` is not supplied and `AWS_ACCESS_KEY_ID` holds a real-looking AWS access key id beginning with `A` (e.g. `AKIAIOSFODNN7EXAMPLE`)
- **THEN** the value's leading `A` is replaced with `L` (e.g. `LKIAIOSFODNN7EXAMPLE`) before it is encoded into the override `access_key`
- **AND** the original (live) key is never written to disk nor sent to LocalStack

#### Scenario: Mock access key from the environment is unchanged

- **WHEN** `--account` is not supplied and `AWS_ACCESS_KEY_ID` holds a value that does not begin with `A` (e.g. `test`)
- **THEN** the value is used as the `access_key` unchanged

#### Scenario: Flag overrides environment

- **WHEN** both `--region` and `AWS_REGION` are set (or both `--account` and `AWS_ACCESS_KEY_ID`)
- **THEN** the flag value takes precedence over the environment variable

#### Scenario: Invalid account is rejected

- **WHEN** a user runs `lstk terraform --account 12345 plan`
- **THEN** the command fails with a clear error stating the account ID must be 12 digits
- **AND** the `terraform` binary is not invoked

#### Scenario: Flag with missing value

- **WHEN** a user runs `lstk terraform --region` with no value following it
- **THEN** the command fails with a clear error stating the flag requires a value

#### Scenario: Flags are a no-op for unproxied subcommands

- **WHEN** a user runs `lstk terraform --region us-west-2 --account 111111111111 validate`
- **THEN** both flags are stripped (leaving `validate`) and not forwarded to `terraform`
- **AND** no override file is generated and the flags have no other effect

#### Scenario: Resolved values take precedence over user provider config

- **WHEN** the user's own `aws` provider block specifies a `region` or `access_key` and `--region`/`--account` (or their env fallbacks) resolve a value
- **THEN** the generated override block's encoded `region`/`access_key` take effect over the user's values


#### Scenario: lstk flags are recognized in any order relative to the tool's own

- **WHEN** a user runs `lstk terraform -chdir=infra --account 111111111111 apply`, or places any argument the wrapped tool owns before an lstk flag
- **THEN** the tool's argument is forwarded unchanged, scanning continues past it, and `--account` is still consumed
- **AND** the result is identical to placing the lstk flag first

#### Scenario: A bare argument after the action ends the leading run

- **WHEN** a user runs `lstk terraform -chdir=infra plan --region us-west-2`
- **THEN** `plan` ends the leading run — it is not absorbed as `-chdir`'s value, because `-chdir=infra` carries its value inline
- **AND** `--region us-west-2` is forwarded to `terraform` unchanged
