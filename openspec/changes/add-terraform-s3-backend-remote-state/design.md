## Context

`lstk terraform` (added in the `add-terraform-command` change) generates a `localstack_providers_override.tf` file that redirects the **AWS provider** to LocalStack, runs the real `terraform`/`tofu`, then deletes the file. Terraform auto-merges any `*_override.tf` over the user's config, so no user files are edited. That change deliberately left two `tflocal` features out (see its design's "Open Questions"): the **S3 state backend** and the **`terraform_remote_state`** data source. This change closes that gap, using `tflocal` (`bin/tflocal`) as the reference.

Two Terraform features still escape the provider override because they are configured outside the `provider "aws"` block and resolve their own AWS endpoints:

```
terraform { backend "s3" { … } }          ← Terraform's own state storage (init-time)
data "terraform_remote_state" "x" {        ← reads another stack's state (plan/apply-time)
  backend = "s3"; config = { … }
}
```

Relevant current state in `internal/iac/terraform/cli/`:
- `exec.go::Run` orchestrates: binary lookup → unproxied check → `EndpointKeys` (schema) → `generateOverride` → run → deferred cleanup.
- `override.go` renders self-contained `provider "aws"` blocks and owns `s3Addressing` (path-style / `s3.` host prefix) and `ensureSafeToWrite`.
- `defaults.go::unproxiedCommands` = `{fmt, validate, version, init}`. `init` is currently unproxied with an explicit "S3-backend support is out of scope" note.
- No AWS SDK linked into the binary; provisioning shells out to the `aws` CLI (via `internal/awscli`). HCL parsing via `github.com/hashicorp/hcl/v2`.

`tflocal` reference behavior we carry over (verified from current `bin/tflocal`): parse `backend "s3"` and `data "terraform_remote_state"` (s3 only) blocks; inject `s3`/`dynamodb`/`iam`/`sts`/`sso` endpoints; pre-create the state bucket and (when configured) the DynamoDB lock table against LocalStack; support both the `endpoints {}` map (Terraform ≥ 1.6) and the legacy flat `*_endpoint` keys; clean up generated overrides afterward.

## Goals / Non-Goals

**Goals:**
- Redirect a `backend "s3"` block to LocalStack so `init`/`plan`/`apply` read and write state in LocalStack.
- Pre-create the configured S3 state bucket, and the DynamoDB lock table when `dynamodb_table` is set, against LocalStack before the backend is initialized.
- Redirect each `data "terraform_remote_state"` block whose `backend = "s3"` to LocalStack, preserving the user's `config` and any `workspace` reference.
- Make `init` conditionally proxied: when an S3 backend is present, generate the backend override, require a running AWS emulator, and provision the bucket/table; otherwise keep today's pass-through.
- Emit the `endpoints {}` map form on modern Terraform/OpenTofu, falling back to legacy flat endpoint keys on older versions.
- Reuse existing machinery: the single generated override file, `s3Addressing`, `ensureSafeToWrite`, deferred cleanup, and the resolved region/account/endpoint plumbing.

**Non-Goals:**
- Backends other than `s3` (local, `gcs`, `azurerm`, `http`, `consul`, …) — untouched, pass through.
- S3 backend authentication features beyond endpoint redirection: `assume_role`, `assume_role_with_web_identity`, `profile`/shared-config resolution, SSO. lstk forces mock credentials, as it does for the provider.
- Multi-bucket / cross-region remote state correctness beyond endpoint redirection (we redirect; we do not reconcile regions/workspaces semantically).
- Migrating existing real-AWS state into LocalStack.

## Decisions

### Reuse the single override file; reproduce the full backend and remote-state blocks
The backend block and any redirected `terraform_remote_state` data blocks are written into the **same** `localstack_providers_override.tf` lstk already generates, appended after the `provider "aws"` blocks. This reuses `ensureSafeToWrite`, the deferred cleanup, and `LSTK_TF_OVERRIDE_FILE_NAME`. Terraform applies override files to the `terraform`/`backend` block and to `data` blocks, but the merge granularity differs from providers — see below.

**Both the backend block and remote-state blocks must be reproduced in full (not partial).** This is the structural difference from the `provider "aws"` override, which is self-contained:
- A `terraform { backend "s3" {} }` in an override file **replaces the user's backend block wholesale** — Terraform does *not* merge backend arguments individually ("the presence of a block defining a backend in an override file always takes precedence"). So a partial override carrying only LocalStack arguments would drop the user's `bucket`/`key`/`region`. lstk must therefore copy every argument from the user's backend block forward and add the LocalStack-specific arguments (endpoints, creds, skip flags), emitting the complete block. This is exactly what `tflocal` does (`default_config.update(backend_config)` then render the whole config).
- For a `data "terraform_remote_state"` block, override merging likewise replaces arguments wholesale, and `config` is a single map argument. Setting `config = {…}` in the override would replace the user's entire config (losing `bucket`/`key`). lstk reproduces the full `config` map — copy every key/value from the user's block and add the endpoint keys — again matching `tflocal`.

So backend and remote-state share one "reproduce everything" strategy; only the provider override is self-contained.

### `init` becomes conditionally proxied
`unproxiedCommands` stays `{fmt, validate, version}` unconditionally. `init` moves to a conditional path:

```
init + no s3 backend present   → pass through, no emulator, no override  (today's behavior)
init + s3 backend present       → require AWS emulator; generate BACKEND-ONLY override;
                                  provision bucket/table; run init
plan/apply/… + s3 backend       → require emulator; generate provider + backend + remote-state
                                  override; provision bucket/table; run
```

Backend redirection does **not** depend on the provider schema (the backend endpoint set is fixed: `s3, dynamodb, iam, sts, sso`), so it works at `init` time before any provider is installed. The provider `endpoints {}` block still requires `terraform init` to have installed the provider, so `init`'s override contains only the backend section, never `provider "aws"` blocks. This removes the "S3-backend support is out of scope" exclusion from the existing "Unproxied and dry-run commands" requirement.

**Detection.** Whether an S3 backend is present is determined by scanning the working-directory `*.tf` files for a `terraform { backend "s3" {} }` block (same HCL parser and recursion rules as provider discovery; backend blocks are only valid in the root module, but recursion is harmless). Presence of this block is what flips `init` onto the proxied path.

**Alternative considered — keep `init` always unproxied and redirect the backend only at plan/apply.** Rejected: `terraform init` is what configures and contacts the backend (it lists the bucket to detect existing state and acquires a lock). If init isn't redirected, it talks to real AWS (or fails) before plan/apply ever runs.

### Provision the state bucket and lock table via the `aws` CLI
A fresh LocalStack has no state bucket, so `init`'s backend setup would fail. lstk pre-creates the resources against LocalStack before running `init`, mirroring `tflocal`'s `get_or_create_bucket`/`get_or_create_ddb_table`:
- **S3 bucket**: create the configured `bucket` if absent (`s3api head-bucket` → `s3api create-bucket`; idempotent — "already owned/exists" is treated as success). Honor the bucket region via `--create-bucket-configuration LocationConstraint=…` for non-`us-east-1`.
- **DynamoDB lock table**: only when the backend sets `dynamodb_table` (legacy locking). Create with hash key `LockID` (S) if absent (`dynamodb describe-table` → `dynamodb create-table`; `ResourceInUseException` treated as success). When the backend uses native S3 locking (`use_lockfile = true`) or no locking, no table is created.

**Implementation — shell out to the `aws` CLI (reuse `internal/awscli`).** Provisioning invokes the real `aws` binary against the resolved LocalStack endpoint (`--endpoint-url`), with forced mock credentials via `awscli.BuildEnv` and `AWS_S3_ADDRESSING_STYLE=path` so the bare endpoint is used verbatim (no virtual-host DNS). Idempotency is decided from exit status (existence checks) and by matching error codes in the combined output (`BucketAlreadyOwnedByYou`/`BucketAlreadyExists`, `ResourceInUseException`). The `aws` CLI is required on PATH only when an S3 backend is declared; a missing binary returns `awscli.ErrNotInstalled`, which `exec.go` surfaces as a friendly install hint.

**Why not the AWS SDK (aws-sdk-go-v2).** An in-process SDK call gives typed idempotency errors and needs no extra binary, but it was the *only* production user of the SDK and pulled the heavy `service/s3` + `service/dynamodb` clients (plus presign/checksum/s3shared/v4a transitive deps) into the released binary to support just two create-if-absent operations. Shelling out keeps the binary lean and matches lstk's established IaC model — `terraform`/`aws`/`cdk`/`sam` are all proxied real tools, so provisioning is no longer the lone in-process exception. The accepted cost is a runtime dependency on `aws` for the S3-backend path (acceptable: anyone running Terraform against an S3 backend almost always has the AWS CLI installed) and stringly-typed error matching instead of typed errors.

Alternatives considered:
- **Keep aws-sdk-go-v2 (config, s3, dynamodb)** — idiomatic and self-contained, but adds a heavy compiled dependency for two operations; rejected in favor of a leaner binary (see above).
- **Raw HTTP / hand-rolled SigV4** against LocalStack — avoids both the SDK and the `aws` dependency, but relies on LocalStack's signature leniency and re-implements request shaping/JSON+XML handling for two services; brittle and not worth it.

### Endpoint emission: `endpoints {}` map vs legacy flat keys (version split)
The S3 backend's endpoint configuration changed shape at **Terraform 1.6.0**, which introduced the nested `endpoints { s3 = …, dynamodb = …, iam = …, sts = …, sso = … }` object and deprecated the flat top-level keys. The exact cutoff is therefore `version < 1.6` → legacy, matching `tflocal` (`is_tf_legacy = TF_VERSION < 1.6`). The legacy key set is **four keys with no `sso`**: `endpoint` (s3), `dynamodb_endpoint`, `iam_endpoint`, `sts_endpoint`. lstk detects the tool version via `<tfBin> version -json` (both `terraform` and `tofu` emit a `terraform_version` field) and emits the appropriate form. The `terraform_remote_state` `config` map uses the same per-version key shape.

**OpenTofu is always modern.** OpenTofu forked from Terraform 1.6, so its minimum version (1.6.0) already has the `endpoints {}` object — `tofu` never takes the legacy path. Legacy handling only ever applies to real `terraform` < 1.6.

**Alternative considered — only support the modern map form and require Terraform ≥ 1.6.** Tempting for simplicity, but `tflocal` supports legacy and lstk should not regress users on older toolchains. The version probe is cheap (one process spawn, only on the proxied backend path) and the legacy key set is small and stable.

### Fixed backend endpoint set (not schema-derived)
Unlike the AWS *provider* (whose endpoint keys are discovered dynamically from `providers schema -json`), the S3 *backend* exposes a small, fixed endpoint surface: `s3`, `dynamodb`, `iam`, `sts`, `sso`. These are hard-coded for the backend/remote-state blocks (matching `tflocal`). This is consistent with the existing "no fallback list" principle, which applies specifically to provider endpoint *discovery* — the backend's endpoint set is part of Terraform's backend schema, not the AWS provider schema, and does not drift per AWS-provider version.

**Only `s3` and `dynamodb` are functionally required; `iam`/`sts`/`sso` are defensive.** With forced static mock credentials plus the skip flags (`skip_credentials_validation`, `skip_metadata_api_check`, `skip_requesting_account_id`), the backend never performs account-id lookup, STS assume-role, or SSO credential resolution, so it never contacts iam/sts/sso. lstk still emits all five in the modern map (matching `tflocal`) because the set is fixed and small, and including them guards against any stray credential code path reaching real AWS — at zero cost. `sso` has no legacy flat key, so it is simply absent on the `< 1.6` path.

### Reuse credentials, region, and S3 addressing
The backend and remote-state blocks reuse the already-resolved `region`/`account` (from `--region`/`--account` and their env fallbacks) and the same `s3Addressing` logic for `use_path_style` and the `s3.` host prefix. The backend block sets `access_key`/`secret_key`, `region`, and the skip flags (`skip_credentials_validation`, `skip_metadata_api_check`, `skip_region_validation`, `skip_requesting_account_id`) so it never contacts real AWS metadata or STS.

### Code organization
New files alongside the existing package, keeping `Run`'s orchestration readable:
- `backend.go` — parse the `backend "s3"` block (bucket, key, region, dynamodb_table, use_lockfile, workspace_key_prefix), render the backend section of the override (version-aware), and detect backend presence.
- `remotestate.go` — parse `data "terraform_remote_state"` blocks with `backend = "s3"`, reproduce each full `config` map with endpoints injected and `workspace` preserved.
- `provision.go` — `aws` CLI helper (reusing `internal/awscli`) to create the bucket (and lock table when configured) against the LocalStack endpoint.
- `version.go` — `<tfBin> version -json` probe → bool/enum selecting map vs legacy endpoint form.
- `exec.go`/`cmd/terraform.go` — thread backend presence into the unproxied/emulator-required decision; `cmd` already resolves the emulator and endpoint, so the change is to also require them for `init` when a backend is present.

## Risks / Trade-offs

- **Backend and remote-state blocks are replaced, not merged** → Override files replace the backend block and the remote-state `config` map wholesale (confirmed against Terraform's documented override behavior), so lstk must reproduce the full block in both cases by copying arbitrary HCL values (strings, bools, nested) forward. Mitigation: copy literal attribute values via HCL exactly as `tflocal` does; document that non-literal/computed values in a backend or remote-state block are out of scope (rare in practice).
- **Bucket region / LocationConstraint** → Creating a bucket in a non-`us-east-1` region requires a `CreateBucketConfiguration`; getting this wrong yields `IllegalLocationConstraint`. Mitigation: set LocationConstraint only when region ≠ `us-east-1`; treat "bucket already exists/owned" as success (idempotent).
- **Locking mode divergence** → Newer configs use `use_lockfile = true` (S3-native locking, no DynamoDB) while older ones use `dynamodb_table`. Creating the wrong resource (or none) breaks locking. Mitigation: create the DynamoDB table **only** when `dynamodb_table` is set; otherwise rely on S3-native locking, no table.
- **Runtime dependency on the `aws` CLI** → Provisioning shells out to `aws`, so the S3-backend path now requires the AWS CLI on PATH (the non-backend terraform path does not). Mitigation: detect a missing binary up front (`awscli.ErrNotInstalled`) and surface a friendly install hint; the dependency is acceptable because Terraform-against-S3 users almost always have the AWS CLI already. Trade-off accepted to avoid linking the heavy aws-sdk-go-v2 S3/DynamoDB clients into the binary.
- **Stringly-typed idempotency** → Without the SDK's typed errors, "already exists/owned" is detected by matching error codes in the `aws` CLI output (`BucketAlreadyOwnedByYou`/`BucketAlreadyExists`, `ResourceInUseException`). Mitigation: match on the stable AWS error-code tokens, not full message text.
- **Tool-version probe parsing** → `version -json` output shape could vary between `terraform` and `tofu`. Mitigation: parse defensively (the `terraform_version` field is common to both); default to the modern `endpoints {}` form when the version can't be parsed (the dominant case for current installs).
- **Workspaces** → `workspace_key_prefix` and non-default workspaces affect the state key path. Mitigation: preserve `workspace_key_prefix`/`workspace` verbatim in the regenerated blocks; correctness beyond endpoint redirection is a non-goal.

## Migration Plan

Additive and backward-compatible. Configurations without an S3 backend behave exactly as today (including `init` pass-through). The only behavior change for existing users is that `lstk terraform init` now requires a running AWS emulator **when the configuration uses an S3 backend** — documented in the command help. No rollback concerns beyond not shipping the feature. Depends on (stacks atop) the `add-terraform-command` change; once that merges to `main`, this branch rebases onto it.

## Open Questions

_All resolved (see Decisions)._

- **Backend override-merge semantics** — *Resolved.* Terraform replaces the backend block wholesale (override always takes precedence; no per-argument merge), so lstk reproduces the **full** backend block, matching `tflocal`. No spike needed. See "Reuse the single override file" decision.
- **Legacy version support cutoff** — *Resolved.* Boundary is Terraform `1.6.0` (`< 1.6` → legacy four-key flat form, no `sso`). Support both; OpenTofu is always ≥ 1.6 and thus always modern. See "Endpoint emission" decision.
- **`sso` endpoint necessity** — *Resolved.* Not functionally required under forced static creds (only `s3`/`dynamodb` are exercised); emitted in the modern map anyway as cheap defensive completeness, matching `tflocal`. See "Fixed backend endpoint set" decision.
