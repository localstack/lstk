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
