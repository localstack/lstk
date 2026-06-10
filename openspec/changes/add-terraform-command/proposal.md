## Why

Users running Terraform against LocalStack today must install and invoke LocalStack's separate Python wrapper, `tflocal`, which generates a provider-override file pointing the AWS provider at LocalStack. lstk already proxies the AWS CLI via `lstk aws`; extending the same pattern to Terraform lets users run `lstk terraform` (and `lstk tf`) with zero extra tooling and with the LocalStack endpoint resolved automatically from the running emulator.

## What Changes

- Add a new `lstk terraform` command (alias `lstk tf`) that proxies all arguments to the underlying `terraform` binary, mirroring the passthrough behavior of `lstk aws`.
- Before invoking `terraform`, generate a Terraform provider-override file (`localstack_providers_override.tf` by default) that configures every `aws` provider/alias block to target the running LocalStack endpoint, then remove it after the command completes.
- Determine the set of AWS service endpoint keys to write into the provider `endpoints {}` block **dynamically** by querying `terraform providers schema -json`, rather than maintaining a hard-coded service list (a key departure from `tflocal`). This works for the Terraform AWS provider version 4.0 and higher. There is **no built-in fallback list**: if no endpoint keys can be discovered the command fails, and if the provider schema is unavailable because `terraform init` has not been run, the command fails with a message telling the user to run `terraform init` first.
- Resolve the LocalStack endpoint automatically via lstk's existing container discovery + `endpoint.ResolveHost`, instead of the host/port environment variables `tflocal` relies on.
- Support Terraform's global `-chdir=DIR` option (in the `-chdir=DIR` form only). When present, lstk anchors all of its directory-relative work — provider-schema discovery, provider-block discovery, override-file write, and cleanup — to `DIR` rather than the process working directory, validates that `DIR` exists up front, and forwards `-chdir=DIR` to `terraform` unchanged so terraform also switches into it. Without this, a proxied command run with `-chdir` would read the schema from and write the override into the wrong directory, silently provisioning against real AWS.
- Support a reduced set of environment variables: `AWS_ENDPOINT_URL`, `LSTK_TF_CMD`, `LSTK_TF_OVERRIDE_FILE_NAME`, `LSTK_TF_DRY_RUN`, `AWS_REGION`, and `AWS_ACCESS_KEY_ID`. lstk-invented variables are renamed from their `tflocal` equivalents (`TF_CMD`/`LS_PROVIDERS_FILE`/`DRY_RUN`) to an `LSTK_TF_`-prefixed form so they don't collide with generic names already present in a user's environment.
- **BREAKING (vs. tflocal):** drop the following `tflocal` environment variables as no longer relevant: `SKIP_ALIASES` (an override block is generated for every discovered `aws` provider/alias), `LOCALSTACK_HOSTNAME`, `EDGE_PORT`, `USE_LEGACY_PORTS`, `S3_HOSTNAME`, `USE_EXEC`, `CUSTOMIZE_ACCESS_KEY`, `AWS_ACCESS_KEY_ID` (for key derivation), `ADDITIONAL_TF_OVERRIDE_LOCATIONS`, `TF_UNPROXIED_CMDS` (the unproxied command set is hard-coded instead), and per-service `<SERVICE>_ENDPOINT` overrides (every endpoint key is written verbatim from the provider schema and points at the single resolved LocalStack endpoint).
- No spinner is shown for this command, since `terraform` is a long-running, streaming command whose own output must remain unobstructed.

## Capabilities

### New Capabilities
- `terraform-proxy`: Running `terraform`/`tofu` against a running LocalStack instance through `lstk`, including automatic endpoint resolution, dynamic provider-override generation from the Terraform provider schema, cleanup of generated files, and a defined set of supported environment variables.

### Modified Capabilities
<!-- None: no existing specs change. -->

## Impact

- **New command wiring**: `cmd/terraform.go` (Cobra command + alias `tf`), registered in the root command.
- **New domain package**: `internal/iac/terraform/cli/` (Go `package cli`, imported as `tfcli`) for binary discovery, override-file generation, schema querying, endpoint mapping, and subprocess execution (analogous to `internal/awscli/`). The `internal/iac/` umbrella reserves space for future IaC tooling and future non-CLI Terraform code.
- **Reused infrastructure**: `internal/container` (`ResolveRunningContainerName`), `internal/endpoint` (`ResolveHost`), `internal/runtime` (Docker health), `internal/output` (sink/events), `internal/config` (emulator container resolution).
- **External dependency**: requires the `terraform` (or `tofu`) binary on `PATH` and the AWS provider (>= 4.0) to be installed via `terraform init` for schema-based endpoint discovery; the command fails with an "run `terraform init`" message otherwise. Adds a Go dependency on `github.com/hashicorp/hcl/v2` for parsing user `.tf` provider blocks.
- **Docs**: update command help to document the new command and the supported/removed environment variables.
