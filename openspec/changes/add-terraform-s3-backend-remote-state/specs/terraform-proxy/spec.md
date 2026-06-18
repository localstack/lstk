## ADDED Requirements

### Requirement: S3 backend redirection

When the working directory declares a `terraform { backend "s3" {} }` block, the system SHALL redirect the backend to the running LocalStack AWS emulator so that Terraform state is read from and written to LocalStack rather than real AWS. The redirection SHALL be applied to every Terraform invocation that initializes or uses the backend, including `init`.

The system SHALL write the backend redirection into the same generated override file used for provider overrides (default `localstack_providers_override.tf`, configurable via `LSTK_TF_OVERRIDE_FILE_NAME`), and SHALL remove it during the same cleanup step. The redirection SHALL set the LocalStack endpoints for `s3`, `dynamodb`, `iam`, `sts`, and `sso`, the resolved `region` and `access_key` (see "Region and account selection"), `secret_key = "test"`, and the skip flags that prevent contact with real AWS metadata, STS, and region/account validation. The S3 endpoint host prefix and path-style behavior SHALL follow the same rules as the provider override ("Provider override file generation").

The backend endpoint set (`s3`, `dynamodb`, `iam`, `sts`, `sso`) SHALL be fixed and SHALL NOT be derived from the AWS provider schema; backend redirection therefore does not require the provider to be installed and works before `terraform init`.

Backends other than `s3` SHALL NOT be modified; the system SHALL forward such configurations to `terraform` unchanged.

#### Scenario: S3 backend present is redirected to LocalStack

- **WHEN** the working directory contains a `terraform { backend "s3" {} }` block and the user runs `lstk terraform init`
- **THEN** the generated override file contains a backend configuration pointing `s3`/`dynamodb`/`iam`/`sts`/`sso` at the resolved LocalStack endpoint with mock credentials and skip flags
- **AND** Terraform initializes state against LocalStack rather than real AWS

#### Scenario: Non-S3 backend is left unchanged

- **WHEN** the working directory declares a backend other than `s3` (for example `local`, `gcs`, or `azurerm`)
- **THEN** the system does not generate a backend redirection
- **AND** the backend configuration is forwarded to `terraform` unchanged

#### Scenario: No backend block declared

- **WHEN** the working directory declares no `backend` block
- **THEN** the system does not generate a backend redirection and behaves as it does today for provider-only proxying

### Requirement: State bucket and lock table provisioning

Before initializing an S3 backend, the system SHALL ensure the backend's resources exist in LocalStack, because a fresh LocalStack instance contains no state bucket. The system SHALL create the configured state `bucket` if it does not already exist, honoring the backend's configured region. When the backend configures DynamoDB-based locking via `dynamodb_table`, the system SHALL create that table if it does not already exist, with the lock schema Terraform expects (hash key `LockID`). When the backend uses S3-native locking (`use_lockfile = true`) or configures no locking, the system SHALL NOT create a DynamoDB table.

Provisioning SHALL be idempotent: an already-existing bucket or table SHALL be treated as success, not an error. Provisioning SHALL target the resolved LocalStack endpoint using mock credentials and SHALL occur before `terraform` is invoked.

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

### Requirement: terraform_remote_state redirection

For each `data "terraform_remote_state"` block whose `backend = "s3"`, the system SHALL redirect the data source to the running LocalStack AWS emulator by writing a regenerated data block into the generated override file. Because Terraform replaces (rather than merges) the `config` map of a data block from an override file, the regenerated block SHALL reproduce the user's full `config` (all keys and values) and add the LocalStack endpoints for `s3`/`dynamodb`/`iam`/`sts`/`sso`. The system SHALL preserve any `workspace` and `workspace_key_prefix` references from the original block so the resolved state key path is unchanged.

`terraform_remote_state` blocks whose backend is not `s3` SHALL be left unchanged.

#### Scenario: S3 remote-state data source is redirected

- **WHEN** the configuration contains a `data "terraform_remote_state" "x"` block with `backend = "s3"` and the user runs `lstk terraform plan`
- **THEN** the generated override file contains a regenerated `data "terraform_remote_state" "x"` block whose `config` reproduces the user's bucket/key/region and adds the LocalStack endpoints
- **AND** the remote state is read from LocalStack rather than real AWS

#### Scenario: Workspace reference is preserved

- **WHEN** an S3 `terraform_remote_state` block sets `workspace` or `workspace_key_prefix`
- **THEN** the regenerated block preserves those values unchanged

#### Scenario: Non-S3 remote-state is left unchanged

- **WHEN** a `terraform_remote_state` block uses a backend other than `s3`
- **THEN** the system does not regenerate it and forwards it to `terraform` unchanged

### Requirement: Terraform version-aware endpoint form

The system SHALL emit S3 backend and `terraform_remote_state` endpoints in the form supported by the in-use Terraform/OpenTofu version. For versions that support the nested `endpoints {}` map (Terraform and OpenTofu 1.6 and later), the system SHALL emit the map form. For older versions, the system SHALL emit the legacy flat keys (`endpoint`, `dynamodb_endpoint`, `iam_endpoint`, `sts_endpoint`). The version SHALL be detected by invoking `<tfBin> version -json`; when the version cannot be determined, the system SHALL default to the modern `endpoints {}` map form.

#### Scenario: Modern version uses the endpoints map

- **WHEN** the in-use Terraform/OpenTofu version supports the `endpoints {}` map
- **THEN** the generated backend and remote-state blocks use the nested `endpoints {}` map form

#### Scenario: Older version uses legacy flat keys

- **WHEN** the in-use Terraform version predates the `endpoints {}` map
- **THEN** the generated backend and remote-state blocks use the flat `endpoint`/`dynamodb_endpoint`/`iam_endpoint`/`sts_endpoint` keys

#### Scenario: Unknown version defaults to the map form

- **WHEN** the Terraform version cannot be determined from `version -json`
- **THEN** the system emits the modern `endpoints {}` map form

## MODIFIED Requirements

### Requirement: Unproxied and dry-run commands

For Terraform subcommands that do not require provider endpoints or backend access (`fmt`, `validate`, `version`), the system SHALL invoke `terraform` directly without generating an override file and without requiring a running LocalStack emulator. The set of unconditionally unproxied subcommands SHALL be fixed and not configurable via environment variable.

`init` SHALL be conditionally proxied based on whether the working directory declares an S3 backend:

- When **no** S3 backend is declared, `init` SHALL pass through directly — no override file, no running emulator required. `init` does not require provider endpoints because the override's provider endpoint keys are discovered from the AWS provider schema, which does not exist until `init` has installed the provider; `init` must therefore pass through to bootstrap the provider for subsequent `plan`/`apply`.
- When an S3 backend **is** declared, `init` SHALL require a running AWS emulator, SHALL generate a backend-only override (the backend redirection, but no `provider "aws"` blocks, since the provider schema is not yet available), and SHALL provision the state bucket and lock table (see "S3 backend redirection" and "State bucket and lock table provisioning") before invoking `terraform init`.

When the `LSTK_TF_DRY_RUN` environment variable is enabled, the system SHALL generate the override file but SHALL NOT invoke `terraform`.

#### Scenario: Unproxied subcommand skips override generation

- **WHEN** a user runs `lstk terraform fmt`
- **THEN** no override file is generated
- **AND** `terraform fmt` is invoked directly

#### Scenario: init without an S3 backend passes through to bootstrap the provider

- **WHEN** a user runs `lstk terraform init` in a configuration with no S3 backend, before the AWS provider is installed
- **THEN** no override file is generated and schema discovery is not attempted
- **AND** `terraform init` is invoked directly so it can install the provider
- **AND** the command does not require a running LocalStack emulator

#### Scenario: init with an S3 backend redirects the backend and provisions resources

- **WHEN** a user runs `lstk terraform init` in a configuration that declares a `backend "s3"`
- **THEN** the command requires a running AWS emulator
- **AND** a backend-only override file (no `provider "aws"` blocks) is generated
- **AND** the state bucket (and the DynamoDB lock table, when `dynamodb_table` is set) is provisioned in LocalStack before `terraform init` runs

#### Scenario: Dry run generates override but does not run terraform

- **WHEN** `LSTK_TF_DRY_RUN` is enabled and a user runs `lstk terraform plan`
- **THEN** the override file is generated and left in place for inspection
- **AND** `terraform` is not invoked
