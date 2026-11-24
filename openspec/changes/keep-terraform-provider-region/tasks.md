## 1. Failing tests first (red)

- [x] 1.1 Add the `aliased-region` sample (`test/integration/test-samples/iac/terraform/`): a default provider in `eu-central-1`, an aliased provider in `us-east-1`, one SNS topic each, with their ARNs as outputs.
- [x] 1.2 Add `TestTerraformOverrideKeepsProviderRegion`: a dry run on the sample plus an alias with no region; only that alias's override block sets `region`. Confirmed failing (3 region lines instead of 1).
- [x] 1.3 Add `TestTerraformE2EProviderRegionIsKept`: apply the sample against real LocalStack and assert each topic's ARN carries its provider's region.

## 2. Implementation (green)

- [x] 2.1 Report per alias whether a root-level block sets `region` (`discoverAWSProviders`), and omit `region` from that alias's override block.
- [x] 2.2 Run the tests from group 1 and confirm they pass.

## 3. Unit coverage

- [x] 3.1 `TestGenerateOverrideKeepsRootProviderRegion`: a root `region = var.region` is kept; a region set only in a sub-directory does not suppress the resolved region.

## 4. Verify

- [x] 4.1 Apply the sample with the built binary against a running LocalStack: the default topic is in `eu-central-1`, the aliased one in `us-east-1`.
- [ ] 4.2 `make lint` and `make test-integration` in a credentialed environment (the e2e test needs `LOCALSTACK_AUTH_TOKEN`).
