## Why

`lstk terraform` currently redirects only the **AWS provider** to LocalStack via a generated `provider "aws"` override. Two common Terraform features are still left pointing at real AWS: the **S3 state backend** (`terraform { backend "s3" {} }`) and the **`terraform_remote_state`** data source. As a result, `lstk terraform init`/`plan`/`apply` against configurations that store state in S3 — or read another stack's state — fail or talk to real AWS, and `init` is explicitly treated as unproxied with "S3-backend support is out of scope". The upstream `tflocal` wrapper already solves this; lstk should reach parity so state-in-S3 workflows work locally.

## What Changes

- **S3 backend redirection.** When the working directory declares `terraform { backend "s3" {} }`, lstk generates a matching backend block in its override file that points S3/DynamoDB/STS/IAM/SSO at the resolved LocalStack endpoint (with credentials, region, and skip flags), so state is read/written in LocalStack.
- **State bucket and lock table provisioning.** Because a fresh LocalStack has no state bucket, lstk pre-creates the configured S3 state bucket (and the DynamoDB lock table when `dynamodb_table` is set) against LocalStack before `init` configures the backend.
- **`terraform_remote_state` redirection.** For each `data "terraform_remote_state"` block whose `backend = "s3"`, lstk regenerates the data block in the override with the user's `config` preserved plus LocalStack endpoints injected, keeping any `workspace` reference intact.
- **`init` becomes conditionally proxied.** When an S3 backend is present, `lstk terraform init` generates the backend portion of the override, requires a running AWS emulator, and provisions the bucket/table — instead of always passing through unproxied. `fmt`/`validate`/`version` remain fully unproxied; configurations with no S3 backend keep today's behavior (init passes through untouched).
- **Terraform/OpenTofu version split.** lstk emits the `endpoints {}` map form for backend/remote-state endpoints on Terraform ≥ 1.6 (and equivalent OpenTofu), falling back to the legacy flat `endpoint`/`dynamodb_endpoint`/`iam_endpoint`/`sts_endpoint` keys on older versions, mirroring `tflocal`.
- **Provisioning via the `aws` CLI.** lstk creates the state bucket and lock table by shelling out to the `aws` CLI (reusing `internal/awscli`), not by linking an AWS SDK — keeping the released binary lean and consistent with lstk's proxy-the-real-tool model. The AWS CLI is required on PATH only when an S3 backend is declared.

## Capabilities

### New Capabilities

_None._ This extends the existing `terraform-proxy` capability rather than introducing a new one.

### Modified Capabilities

- `terraform-proxy`: Adds requirements for S3 backend redirection, state bucket / lock table provisioning, and `terraform_remote_state` redirection; and modifies the "Unproxied and dry-run commands" requirement so that `init` is conditionally proxied when an S3 backend is present (removing the current "S3-backend support is out of scope" exclusion for `init`).

## Impact

- **Code**: `internal/iac/terraform/cli/` — new backend/remote-state parsing and override generation (alongside existing `override.go`/`schema.go`), tool-version detection, and an `aws` CLI helper (reusing `internal/awscli`) to provision the bucket/table; `internal/iac/terraform/cli/exec.go` and `cmd/terraform.go` to make `init` conditionally require a running emulator and endpoint.
- **Dependencies**: no new compiled dependency — provisioning reuses the `aws` CLI via `internal/awscli`. The `aws` binary becomes a runtime requirement for the S3-backend path only.
- **Behavior**: `lstk terraform init` now requires a running AWS emulator (and the `aws` CLI on PATH) when the configuration uses an S3 backend (previously never required for `init`). No change for configurations without an S3 backend.
- **Tests**: new integration/e2e coverage for state-in-S3 init/plan/apply and remote-state reads; unit tests for backend/remote-state parsing and version-split endpoint emission.
- **Out of scope**: non-S3 backends (local, gcs, azurerm, etc.); S3 backend `assume_role`/profile features beyond endpoint redirection.
