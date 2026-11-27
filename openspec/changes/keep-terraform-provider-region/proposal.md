## Why

`lstk terraform` writes the resolved region (`--region`, then `AWS_REGION`, then `us-east-1`) into every generated `provider "aws"` override block. Terraform override files replace attributes one by one, so this replaces the region of every provider, including one that pins its own. A configuration that needs an aliased provider in a different region, e.g. `aws.us_east_1` for CloudFront's ACM certificate and Lambda@Edge while the rest of the stack is in `eu-central-1`, ends up with everything in one region. Creating a CloudFront distribution with a Lambda@Edge association then fails with `InvalidLambdaFunctionAssociation: The function must be in region 'us-east-1'`.

This happens even without `--region`: the resolution always yields a value, so with no flag and no env var every provider is moved to `us-east-1`. Plain `terraform` with `AWS_ENDPOINT_URL` keeps each provider's own region, which is what users fall back to today.

## What Changes

- A generated provider override block sets `region` only when no root-level `provider "aws"` block with the same alias sets `region` itself (as a literal or any expression). Otherwise the user's region stands, as with plain `terraform`.
- Only blocks in the working directory itself count. An override file merges only with provider blocks in its own module directory, so a region set in a sub-directory never suppresses the root override's region.
- `access_key`, `secret_key`, the skip flags and the `endpoints {}` block are unchanged and still always written.
- **Behaviour change**: `--region` (and `AWS_REGION`) no longer overrides a region the user's provider block sets explicitly. This matches `AWS_REGION=… terraform`.

## Non-goals

- Reworking `--region` into an `AWS_REGION` passthrough for the `terraform` subprocess, or honouring a shared-config profile region. Both would bring lstk closer to plain `terraform`, but are separate changes.
- Backend and `terraform_remote_state` redirection: they already write the resolved region only when the user sets none.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `terraform-proxy`: "Provider override file generation" encodes the resolved region only into blocks that set none; "Region and account selection" stops claiming the resolved region takes precedence over the user's provider config.

## Impact

- `internal/iac/terraform/cli/override.go`: `discoverAWSAliases` becomes `discoverAWSProviders`, reporting per alias whether a root-level block sets `region`; `writeProviderBlock` omits `region` when it is empty.
- `internal/iac/terraform/cli/override_test.go`: unit coverage for root-only detection.
- `test/integration/`: a dry-run override test and a real-LocalStack e2e test on a new `aliased-region` sample.
- Release: user-visible behaviour change, `semver:minor`.
