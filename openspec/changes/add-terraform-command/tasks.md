## 1. Domain package scaffolding

- [x] 1.1 Create `internal/iac/terraform/cli/` package (Go `package cli`, imported as `tfcli`) with a `Run(ctx, endpointURL, region, account string, sink output.Sink, logger log.Logger, args []string) error` entry point signature
- [x] 1.2 Add `defaults.go` with the fixed passthrough-command set (`fmt`, `validate`, `version`, `init` — `init` bootstraps the provider so it runs directly without schema discovery). No fallback endpoint-key list — discovery is schema-only.
- [x] 1.3 Add env-var helpers scoped to the package: `tfCmd()` (`LSTK_TF_CMD`, default `terraform`), `overrideFileName()` (`LSTK_TF_OVERRIDE_FILE_NAME`, default `localstack_providers_override.tf`), `dryRun()` (`LSTK_TF_DRY_RUN`), and `endpointURLOverride()` (`AWS_ENDPOINT_URL`). lstk-invented vars use the `LSTK_TF_` prefix. Do NOT read `AWS_DEFAULT_REGION`, and do NOT support `SKIP_ALIASES`.

## 2. Endpoint schema discovery

- [x] 2.1 Implement `schema.go`: run `<tfBin> providers schema -json`, parse JSON, navigate `provider_schemas["registry.terraform.io/hashicorp/aws"].provider.block.block_types["endpoints"].block.attributes`, return the endpoint key names verbatim (no case transformation). Supports AWS provider 4.0+.
- [x] 2.2 No fallback list: when the `providers schema` query fails or the AWS provider is absent from `provider_schemas`, return a dedicated `ErrInitRequired` whose message tells the user to run `terraform init` first; when the schema is present but yields no endpoint keys, return a plain error. The caller fails in both cases rather than generating an override.
- [x] 2.3 Unit test schema parsing against a captured `terraform providers schema -json` fixture (AWS provider 4.0+); assert the missing-provider / failed-query case returns `ErrInitRequired`, and the empty-keys case returns an error

## 3. Override file generation

- [x] 3.1 Implement `override.go`: recursively discover `aws` provider blocks (and their `alias`/`region`) across the working-directory tree's `*.tf` files (`filepath.WalkDir`) using `github.com/hashicorp/hcl/v2` (`hclparse`), skipping hidden directories such as `.terraform`/`.git`; log and skip any individual file that fails to parse, and fall back to a single alias-less provider override when no provider block is found. Unit test that a provider block in a sub-directory is discovered and that `.terraform` is ignored
- [x] 3.2 Render one `provider "aws"` block per discovered alias with the resolved `access_key` (target account) and `region` encoded in, `secret_key = "test"`, `skip_credentials_validation`, `skip_metadata_api_check`, preserved `alias`, and the `endpoints {}` block
- [x] 3.3 Compute `s3_use_path_style` from the resolved host (true when the host is not a virtual-host-capable `*.localstack.cloud` host, e.g. `127.0.0.1`/`localhost`). When path style is off (virtual-host-capable host), special-case the `s3` endpoint key to prefix its host with `s3.` (e.g. `s3.localhost.localstack.cloud`), unless the host already begins with `s3.`; all other endpoints use the resolved endpoint unchanged
- [x] 3.4 Write every schema-derived endpoint key into the `endpoints {}` block verbatim, all mapped to the single resolved LocalStack endpoint (no per-service `<SERVICE>_ENDPOINT` overrides)
- [x] 3.5 Implement the pre-existing-file safety check: fail with a clear error if the target override file already exists (lstk keeps no record of authorship, so any existing file is treated as a conflict — user-authored or orphaned by a prior run); the error tells the user to remove it or set `LSTK_TF_OVERRIDE_FILE_NAME`
- [x] 3.6 Return written file paths for cleanup; unit test override rendering (default provider, multiple aliases, custom file name, path-style on/off, and the `s3` host prefix on/off — prefixed for virtual-host-capable hosts, bare for `127.0.0.1`/`localhost`)

## 4. Region and account flag handling (cmd boundary)

- [x] 4.1 Add a boundary-level parser (generalize `stripNonInteractiveFlag`) that extracts `--region` and `--account` only in leading position — scan from the start of the terraform args, accept both `--flag value` and `--flag=value` forms, and STOP at the first token that is not one of these flags/values (everything after is forwarded verbatim); return a clear error when a leading flag has no following value. Do NOT register them as root/persistent Cobra flags.
- [x] 4.2 Validate `--account` against `^\d{12}$`, returning a clear error when invalid; do not validate `AWS_ACCESS_KEY_ID`, but run it through `DeactivateAccessKey` (leading `A` → `L`) so a real AWS key (`AKIA…`/`ASIA…`) in the env is neutralized before it is encoded into the override or sent to LocalStack. Unit test the helper (real long-term/session keys deactivated; `test`/12-digit/empty untouched)
- [x] 4.3 Resolve effective region (`--region` → `AWS_REGION` → `us-east-1`) and account (`--account` → `AWS_ACCESS_KEY_ID` → `test`) at the cmd boundary and pass into `tfcli.Run`
- [x] 4.4 For unproxied subcommands, strip the flags but treat them as a no-op (no override generated, flags not forwarded)
- [x] 4.5 Unit test the parser/validator: both flag forms, missing value, invalid account, env fallback, flag-over-env precedence, default fallback, and leading-only behavior (flag after the action is left untouched in the forwarded args; scanning stops at the first non-flag token)

## 5. Execution orchestration

- [x] 5.1 In `exec.go`, resolve the binary via `exec.LookPath(tfCmd())`; on failure return a clear "install terraform / available on PATH" error
- [x] 5.2 Detect unproxied subcommands (first non-flag arg in the fixed set) and run terraform directly without generating an override
- [x] 5.3 For proxied commands: discover endpoint keys from the schema (fail with the `ErrInitRequired` "run terraform init" message when unavailable, or a plain error when no keys are found — no fallback), generate the override file, and register a `defer` that removes only lstk-written files (runs on success, error, and context cancellation)
- [x] 5.4 Honor `LSTK_TF_DRY_RUN`: generate the override file and skip the terraform invocation
- [x] 5.5 Run terraform via `exec.CommandContext` with `os.Stdin` and the passed stdout/stderr writers; wrap non-zero exit as `output.NewSilentError`; do NOT wrap output in a spinner writer
- [x] 5.6 Add OpenTelemetry span around execution (mirror `internal/awscli/exec.go`)

## 6. Command wiring

- [x] 6.1 Add `cmd/terraform.go` with `newTerraformCmd(cfg *env.Env)`: `Use: "terraform [args...]"`, `Aliases: []string{"tf"}`, `DisableFlagParsing: true`, `PreRunE: initConfig(nil)`, and help text documenting `--region`/`--account` and supported/removed env vars
- [x] 6.2 In `RunE`, reuse the `aws.go` preamble (`stripNonInteractiveFlag`, `runtime.NewDockerRuntime`, resolve `config.EmulatorAWS` `ContainerConfig`, `rt.IsHealthy`, `container.ResolveRunningContainerName`, `endpoint.ResolveHost`) and add the `--region`/`--account` parse + resolve from section 4
- [x] 6.3 Build `http://host:port`, create a `PlainSink`, obtain the logger, and call `tfcli.Run(...)` (aliased import) with the resolved region and account
- [x] 6.4 Register the command in the root command alongside `newAWSCmd`

## 7. Tests

- [x] 7.1 Integration test: `lstk terraform`/`lstk tf` forwards args and propagates exit code (using a stub terraform binary on `PATH` and isolated `$HOME` via `testEnvWithHome`)
- [x] 7.2 Integration test: fails with "not running" error and does not invoke terraform when no emulator is running
- [x] 7.3 Integration test: `fmt`/`validate`/`version`/`init` run without generating an override file and without requiring a running emulator (including with `--region`/`--account` present, which are stripped and ignored)
- [x] 7.4 Integration test: `LSTK_TF_DRY_RUN` generates the override file and skips terraform; assert resolved `region`/`access_key` are encoded in the override
- [x] 7.5 Integration test: pre-existing override file causes a clear failure and is not deleted
- [x] 7.6 Integration test: leading `--region`/`--account` are stripped from forwarded args and encoded into the override; invalid `--account` fails before terraform is invoked
- [x] 7.7 Integration test: positional rules — `--region`/`--account` placed after the terraform action are forwarded to terraform unchanged (not consumed by lstk), and placing them before the `terraform` subcommand (`lstk --account … terraform`) is rejected
- [x] 7.8 Integration test (Docker-gated): override file is generated and removed after a real `terraform plan` against a running LocalStack, with endpoint keys sourced from the provider schema
- [x] 7.9 Integration test: when the provider schema is unavailable (no `terraform init` / provider not installed), the command fails with the specific "run `terraform init` first" message and does not invoke terraform or generate an override
- [x] 7.10 Integration test: `LSTK_TF_CMD=tofu` causes lstk to invoke a `tofu` stub on `PATH` instead of `terraform`

## 8. End-to-end matrix (real terraform + AWS provider + LocalStack)

The tests in section 7 use a stub `terraform` and an alpine stand-in for the
emulator, so they run fast and need no downloads. The cases below instead
exercise the real `terraform`/`tofu` binary, a real AWS provider installed via
`terraform init`, and a real LocalStack container (`localstack/localstack-pro`,
lstk's default AWS emulator, activated with `LOCALSTACK_AUTH_TOKEN`). They live
in `test/integration/terraform_e2e_test.go`, gated on Docker + a real
`terraform`/`tofu` binary + an auth token (CI installs Terraform and OpenTofu on
the Linux shards via `hashicorp/setup-terraform` / `opentofu/setup-opentofu`;
the tests skip wherever a prerequisite is absent, e.g. macOS/Windows).

**`apply`, not bare `plan`.** The matrix was framed around `terraform plan`, but a create-only `plan` against empty state makes no API calls (and the provider skips credential/metadata checks), so it would never touch LocalStack and couldn't validate the endpoint override. The routing-sensitive cases therefore run `terraform apply -auto-approve` of a single S3 bucket (a community service): a successful apply proves `CreateBucket` was routed through the generated override to LocalStack. Discovery/precedence assertions that don't need a live call use an `LSTK_TF_DRY_RUN` capture of the override file.

- [x] 8.1 Harness: a Docker+token+terraform-gated helper that starts a real LocalStack named `localstack-aws` (4566 bound to 127.0.0.1, so the host-side terraform reaches it), waits for `/_localstack/health`, copies a sample project into a temp dir, runs real `lstk terraform init`, and asserts the override is generated during the run and removed after. Sample `.tf` projects live as readable files under `test/integration/test-samples/iac/terraform/<name>/` and are copied into a temp working dir per test (`copySample`) so terraform/lstk never write into the tracked tree
- [x] 8.2 Top-level project: all `.tf` in a single directory — `init` + `apply` succeed against LocalStack and the override is cleaned up
- [x] 8.3 Sub-directories: an aliased provider in a sub-directory is represented in the override (captured via `LSTK_TF_DRY_RUN` against the real schema), while a provider block under `.terraform` is ignored
- [x] 8.4 Single `aws` provider block (no alias) — exactly one override block (dry-run capture), `apply` succeeds
- [x] 8.5 Multiple aliased `aws` provider blocks alongside the default block — one override block per alias plus the default, and `apply` through both providers succeeds against LocalStack
- [x] 8.6 Provider block that explicitly sets an endpoint (FIPS) — the generated override points S3 at LocalStack (dry-run capture asserts the FIPS endpoint did not survive) and `apply` succeeds (it would fail against the public FIPS endpoint with mock creds)
- [x] 8.7 AWS provider version coverage: oldest supported (`~> 4.0`) and the latest provider release — `apply` (which requires schema-based endpoint discovery) succeeds for both
- [x] 8.8 Top-level flow under `tofu` (`LSTK_TF_CMD=tofu`), since behavior may differ subtly from `terraform`

## 9. Documentation

- [x] 9.1 Ensure command `Long` help lists `--region`/`--account` and supported env vars, documents that LocalStack must be running, and notes that `terraform init` must have been run (so the provider schema is available)
- [x] 9.2 Document `-chdir=DIR` support in the command `Long` help (the `-chdir=DIR` form only, anchors lstk's directory-relative work to `DIR`)

## 11. Working directory selection (`-chdir`)

- [x] 11.1 Add a leading-token parser for `-chdir=DIR` (the `=` form only): recognize it during the leading-flag scan alongside `--region`/`--account`, record `DIR`, and CONTINUE scanning past it (do not stop, and do not strip it — it must remain in the forwarded args). A space-separated `-chdir DIR` is not recognized and is forwarded verbatim
- [x] 11.2 Compute the effective working directory from `DIR` (absolute used as-is; relative joined to `os.Getwd()`) and thread it in place of `os.Getwd()` through `EndpointKeys`, `generateOverride`/`discoverAWSAliases`, and the cleanup `defer`. The schema probe continues to scope via `cmd.Dir` (no `-chdir` re-injection); the real terraform run keeps `-chdir=DIR` in its args so terraform also switches
- [x] 11.3 Validate the effective working directory exists (`os.Stat`) before any work; on failure emit an `ErrorEvent` naming the missing directory and return a silent error without invoking terraform or generating an override
- [x] 11.4 Integration test: `lstk terraform -chdir=DIR` proxied run discovers schema from / writes the override into / cleans up `DIR`, and forwards `-chdir=DIR` to terraform (dry-run capture is sufficient to assert override location and contents)
- [x] 11.5 Integration test: a nonexistent `-chdir` directory fails with a clear error and does not invoke terraform or generate an override
- [x] 11.6 Unit test the parser: `-chdir=DIR` is recognized and retained while interleaved `--region`/`--account` (on either side) are still consumed and removed; the space form `-chdir DIR` is left untouched in the forwarded args

## 10. Post-review refinements

- [x] 10.1 Remove the `e2eContext` helper and use the default `testContext` (2 min) in the e2e tests; if the cold-shard image pull makes 2 min tight, add a CI step that pre-pulls `localstack/localstack-pro` rather than lengthening the per-test budget
- [x] 10.2 Stop calling `runLstk` directly for terraform in the e2e tests — add a thin `runTerraform(t, ctx, work, e, args...)` wrapper (prepends `terraform`) and route all init/plan/apply/dry-run calls through it (replacing `runTFInit` too), so the tests read in terraform terms
- [x] 10.3 Make the init-required error generic and end-user-friendly: `ErrInitRequired` should just tell the user to run `terraform init`, with no mention of reading the provider schema (code-only; the spec scenario already requires generic wording)
- [x] 10.4 Restrict `lstk terraform` to the AWS emulator: when the AWS emulator isn't running, check `container.RunningEmulators(...)` and, if a non-AWS emulator (Snowflake/Azure) is up, fail with an AWS-specific error naming it instead of the generic "AWS not running"; add the integration test for the non-AWS-emulator-running case
- [x] 10.5 Filter mutually-exclusive endpoint-key aliases. The provider warns ("Invalid Attribute Combination … Only one of the following attributes should be set …", future error) when multiple aliases of the same service are set — surfaced on `terraform plan` (with a resource forcing provider configuration), not `validate`. The schema carries no conflict metadata, so `dedupeAliasKeys` (schema.go) drops all but the first present member of each group in a maintained `endpointAliasGroups` table, captured authoritatively from `terraform plan -json` on AWS provider 6.48 (39 groups incl. `databrew`/`gluedatabrew` and `lexmodels`/`lex`) ∪ `simpledb`/`sdb`. Verified end-to-end: 313→265 keys, 0 remaining warnings. Unit-tested.
- [x] 10.6 Bring up the real LocalStack in the e2e tests via `lstk start` instead of driving the Docker SDK directly. `startRealLocalStack` (in `test/integration/localstack_test.go`) reimplements what `lstk start` already does — pull the image, bind 4566 to 127.0.0.1, inject `LOCALSTACK_AUTH_TOKEN`, poll `/_localstack/health` — so replace its body with a single `runLstk(..., "start")` call: `lstk start` resolves the same default image (`localstack/localstack-pro:latest`), binds the loopback in `internal/runtime/docker.go`, and blocks on readiness via `awaitStartup`, making `waitForLocalStackReady` redundant. Drops the Docker SDK plumbing (and the `realLocalStackImage` const + `waitForLocalStackReady` helper) from the harness and exercises the real user bring-up path. Trade-off accepted: a regression in `start` now also surfaces in the terraform e2e suite.
