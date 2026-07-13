## 1. Open questions — resolved (see design.md)

- [x] 1.1 Backend override-merge: Terraform replaces the backend block wholesale (no per-argument merge), so reproduce the **full** backend block, matching `tflocal`. No spike needed.
- [x] 1.2 Version boundary: legacy is Terraform `< 1.6.0` (four-key flat form, no `sso`); modern is `≥ 1.6` `endpoints {}` map. Parse the `terraform_version` field from `version -json` (emitted by both `terraform` and `tofu`; OpenTofu is always ≥ 1.6).
- [x] 1.3 `sso` endpoint: not functionally required under forced static creds (only `s3`/`dynamodb` are exercised); keep the fixed set `{s3, dynamodb, iam, sts, sso}` in the modern map as cheap defensive completeness, `sso` absent on the legacy path.

## 2. Dependencies

- [x] 2.1 No new compiled dependency: provisioning reuses the `aws` CLI via `internal/awscli`, so the released binary links no AWS SDK. The `aws` binary is a runtime requirement for the S3-backend path only.

## 3. Tool version detection

- [x] 3.1 Add `version.go`: run `<tfBin> version -json` and parse the version into a selector for map-vs-legacy endpoint form; default to the modern map form when the version cannot be determined.
- [x] 3.2 Unit-test version parsing: modern terraform, modern tofu, an older legacy version, and unparseable output (defaults to map form).

## 4. S3 backend parsing and detection

- [x] 4.1 Add `backend.go`: parse the `terraform { backend "s3" {} }` block from the working directory's `*.tf` files (HCL, same recursion/skip rules as provider discovery), extracting `bucket`, `key`, `region`, `dynamodb_table`, `use_lockfile`, `workspace_key_prefix`.
- [x] 4.2 Add a `HasS3Backend(workdir)` (or equivalent) detector used to flip `init` onto the proxied path.
- [x] 4.3 Unit-test backend parsing: s3 backend present (all fields), s3 backend with only required fields, a non-s3 backend (detector returns false), and no backend block.

## 5. Backend override rendering

- [x] 5.1 Render the backend section of the override file (version-aware: `endpoints {}` map vs legacy flat keys) with `s3`/`dynamodb`/`iam`/`sts`/`sso` endpoints, resolved `region`/`access_key`, `secret_key`, and skip flags; reuse `s3Addressing` for path-style and the `s3.` host prefix. Reproduce the **full** backend block (copy all user args forward + add LocalStack args), since override files replace the backend block wholesale.
- [x] 5.2 Wire backend rendering into `generateOverride`: append the backend section after the `provider "aws"` blocks, and support a backend-only mode (no provider blocks) for `init`.
- [x] 5.3 Unit-test rendered backend output for both endpoint forms and for both S3 addressing modes (path-style host vs virtual-host `*.localstack.cloud`).

## 6. terraform_remote_state redirection

- [x] 6.1 Add `remotestate.go`: parse `data "terraform_remote_state"` blocks with `backend = "s3"`, reproducing the full `config` map (all user keys/values) with `s3`/`dynamodb`/`iam`/`sts`/`sso` endpoints injected and `workspace`/`workspace_key_prefix` preserved.
- [x] 6.2 Render regenerated `data "terraform_remote_state"` blocks into the override file (version-aware endpoint form); leave non-s3 remote-state blocks untouched.
- [x] 6.3 Unit-test: s3 remote-state with workspace preserved, s3 remote-state minimal config, and a non-s3 remote-state (not regenerated).

## 7. Resource provisioning (`aws` CLI)

- [x] 7.1 Add `provision.go`: shell out to the `aws` CLI (reusing `internal/awscli` for the binary check and forced mock-credential env) against the resolved LocalStack endpoint with `AWS_S3_ADDRESSING_STYLE=path`; surface `awscli.ErrNotInstalled` as a friendly install hint from `exec.go`.
- [x] 7.2 Create the state bucket if absent (`s3api head-bucket` → `s3api create-bucket`; idempotent; honor region via `--create-bucket-configuration LocationConstraint=…` only when region ≠ `us-east-1`; treat already-exists/owned output as success).
- [x] 7.3 Create the DynamoDB lock table (hash key `LockID`, type S) only when `dynamodb_table` is set and the table is absent (`dynamodb describe-table` → `dynamodb create-table`; `ResourceInUseException` treated as success).
- [x] 7.4 Unit-test provisioning decisions (table created only when `dynamodb_table` set; LocationConstraint only for non-default region; idempotency error-code matching) with a fake `aws` runner.

## 8. Orchestration wiring

- [x] 8.1 Update `defaults.go`/`exec.go` so `init` is conditionally proxied: pass through when no S3 backend; otherwise generate a backend-only override, require the resolved endpoint, and provision resources before running `init`.
- [x] 8.2 Update `cmd/terraform.go` so that `init` with an S3 backend present takes the emulator-required path (health check, running-container resolution, endpoint resolution) instead of the unproxied shortcut.
- [x] 8.3 In `Run`, sequence the proxied path: detect backend → provision bucket/table → generate override (provider + backend + remote-state) → run terraform → deferred cleanup. Ensure provisioning happens before `terraform` runs and cleanup still removes the override on non-zero exit.
- [x] 8.4 Ensure dry-run (`LSTK_TF_DRY_RUN`) still generates the full override (provider + backend + remote-state) without running terraform and without provisioning, or document the chosen dry-run behavior for provisioning.

## 9. Unit tests (package-level)

- [x] 9.1 Verify the unconditionally-unproxied set is `fmt`/`validate`/`version` and that `init` routing depends on backend presence.
- [x] 9.2 Verify the generated override never contains a pre-existing-file clobber (reuse `ensureSafeToWrite`) when backend/remote-state sections are added.

## 10. Integration / e2e tests

- [x] 10.1 Add a test sample under `test/integration/test-samples/iac/terraform/s3-backend/` with a `backend "s3"` and a single resource; assert `lstk tf init` + `apply` succeeds against a real LocalStack and state lands in the LocalStack bucket.
- [x] 10.2 Add a sample with `dynamodb_table` locking; assert the lock table is created and apply succeeds.
- [x] 10.3 Add a sample exercising `data "terraform_remote_state"` reading another stack's S3 state; assert the value is read from LocalStack.
- [x] 10.4 Add an OpenTofu (`LSTK_TF_CMD=tofu`) variant for at least one backend case to cover the version-aware endpoint form.
- [x] 10.5 Confirm `init`/`apply` with no S3 backend still behaves as before (no emulator required for a backend-less `init`).

## 11. Docs and spec sync

- [x] 11.1 Update `cmd/terraform.go` help text to document S3 backend / remote-state support and the new "`init` requires a running emulator when an S3 backend is present" behavior.
- [x] 11.2 Update `CLAUDE.md` (Terraform section) and remove/adjust any "S3-backend support is out of scope" wording left from the parent change.
- [x] 11.3 Run `make lint`, `make test`, and `make test-integration`; ensure green before opening the PR.
