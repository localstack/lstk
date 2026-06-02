## 1. Domain package scaffolding

- [ ] 1.1 Create `internal/iac/terraform/cli/` package (Go `package cli`, imported as `tfcli`) with a `Run(ctx, endpointURL, region, account string, sink output.Sink, logger log.Logger, args []string) error` entry point signature
- [ ] 1.2 Add `defaults.go` with the fixed unproxied-command set (`fmt`, `validate`, `version`). No fallback endpoint-key list — discovery is schema-only.
- [ ] 1.3 Add env-var helpers scoped to the package: `tfCmd()` (`TF_CMD`, default `terraform`), `overrideFileName()` (`LS_PROVIDERS_FILE`, default `localstack_providers_override.tf`), `dryRun()` (`DRY_RUN`), `skipAliases()` (`SKIP_ALIASES`), and `endpointURLOverride()` (`AWS_ENDPOINT_URL`). Do NOT read `AWS_DEFAULT_REGION`.

## 2. Endpoint schema discovery

- [ ] 2.1 Implement `schema.go`: run `<tfBin> providers schema -json`, parse JSON, navigate `provider_schemas["registry.terraform.io/hashicorp/aws"].provider.block.block_types["endpoints"].block.attributes`, return the endpoint key names verbatim (no case transformation). Supports AWS provider 4.0+.
- [ ] 2.2 No fallback list: when the `providers schema` query fails or the AWS provider is absent from `provider_schemas`, return a dedicated `ErrInitRequired` whose message tells the user to run `terraform init` first; when the schema is present but yields no endpoint keys, return a plain error. The caller fails in both cases rather than generating an override.
- [ ] 2.3 Unit test schema parsing against a captured `terraform providers schema -json` fixture (AWS provider 4.0+); assert the missing-provider / failed-query case returns `ErrInitRequired`, and the empty-keys case returns an error

## 3. Override file generation

- [ ] 3.1 Implement `override.go`: discover `aws` provider blocks (and their `alias`/`region`) in the working directory's `*.tf` files using `github.com/hashicorp/hcl/v2` (`hclparse`); on parse failure, fall back to a single alias-less provider override and log the degradation
- [ ] 3.2 Render one `provider "aws"` block per non-skipped alias with the resolved `access_key` (target account) and `region` encoded in, `secret_key = "test"`, `skip_credentials_validation`, `skip_metadata_api_check`, preserved `alias`, and the `endpoints {}` block
- [ ] 3.3 Compute `s3_use_path_style` from the resolved host (true when the host is not a virtual-host-capable `*.localstack.cloud` host, e.g. `127.0.0.1`/`localhost`). When path style is off (virtual-host-capable host), special-case the `s3` endpoint key to prefix its host with `s3.` (e.g. `s3.localhost.localstack.cloud`), unless the host already begins with `s3.`; all other endpoints use the resolved endpoint unchanged
- [ ] 3.4 Write every schema-derived endpoint key into the `endpoints {}` block verbatim, all mapped to the single resolved LocalStack endpoint (no per-service `<SERVICE>_ENDPOINT` overrides)
- [ ] 3.5 Implement the pre-existing-file safety check: fail with a clear error if the target override file already exists and was not created by lstk
- [ ] 3.6 Return written file paths for cleanup; unit test override rendering (default provider, multiple aliases, SKIP_ALIASES, custom file name, path-style on/off, and the `s3` host prefix on/off — prefixed for virtual-host-capable hosts, bare for `127.0.0.1`/`localhost`)

## 4. Region and account flag handling (cmd boundary)

- [ ] 4.1 Add a boundary-level parser (generalize `stripNonInteractiveFlag`) that extracts `--region` and `--account` only in leading position — scan from the start of the terraform args, accept both `--flag value` and `--flag=value` forms, and STOP at the first token that is not one of these flags/values (everything after is forwarded verbatim); return a clear error when a leading flag has no following value. Do NOT register them as root/persistent Cobra flags.
- [ ] 4.2 Validate `--account` against `^\d{12}$`, returning a clear error when invalid; do not validate `AWS_ACCESS_KEY_ID`
- [ ] 4.3 Resolve effective region (`--region` → `AWS_REGION` → `us-east-1`) and account (`--account` → `AWS_ACCESS_KEY_ID` → `test`) at the cmd boundary and pass into `tfcli.Run`
- [ ] 4.4 For unproxied subcommands, strip the flags but treat them as a no-op (no override generated, flags not forwarded)
- [ ] 4.5 Unit test the parser/validator: both flag forms, missing value, invalid account, env fallback, flag-over-env precedence, default fallback, and leading-only behavior (flag after the action is left untouched in the forwarded args; scanning stops at the first non-flag token)

## 5. Execution orchestration

- [ ] 5.1 In `exec.go`, resolve the binary via `exec.LookPath(tfCmd())`; on failure return a clear "install terraform / available on PATH" error
- [ ] 5.2 Detect unproxied subcommands (first non-flag arg in the fixed set) and run terraform directly without generating an override
- [ ] 5.3 For proxied commands: discover endpoint keys from the schema (fail with the `ErrInitRequired` "run terraform init" message when unavailable, or a plain error when no keys are found — no fallback), generate the override file, and register a `defer` that removes only lstk-written files (runs on success, error, and context cancellation)
- [ ] 5.4 Honor `DRY_RUN`: generate the override file and skip the terraform invocation
- [ ] 5.5 Run terraform via `exec.CommandContext` with `os.Stdin` and the passed stdout/stderr writers; wrap non-zero exit as `output.NewSilentError`; do NOT wrap output in a spinner writer
- [ ] 5.6 Add OpenTelemetry span around execution (mirror `internal/awscli/exec.go`)

## 6. Command wiring

- [ ] 6.1 Add `cmd/terraform.go` with `newTerraformCmd(cfg *env.Env)`: `Use: "terraform [args...]"`, `Aliases: []string{"tf"}`, `DisableFlagParsing: true`, `PreRunE: initConfig(nil)`, and help text documenting `--region`/`--account` and supported/removed env vars
- [ ] 6.2 In `RunE`, reuse the `aws.go` preamble (`stripNonInteractiveFlag`, `runtime.NewDockerRuntime`, resolve `config.EmulatorAWS` `ContainerConfig`, `rt.IsHealthy`, `container.ResolveRunningContainerName`, `endpoint.ResolveHost`) and add the `--region`/`--account` parse + resolve from section 4
- [ ] 6.3 Build `http://host:port`, create a `PlainSink`, obtain the logger, and call `tfcli.Run(...)` (aliased import) with the resolved region and account
- [ ] 6.4 Register the command in the root command alongside `newAWSCmd`

## 7. Tests

- [ ] 7.1 Integration test: `lstk terraform`/`lstk tf` forwards args and propagates exit code (using a stub terraform binary on `PATH` and isolated `$HOME` via `testEnvWithHome`)
- [ ] 7.2 Integration test: fails with "not running" error and does not invoke terraform when no emulator is running
- [ ] 7.3 Integration test: `fmt`/`validate`/`version` run without generating an override file (including with `--region`/`--account` present, which are stripped and ignored)
- [ ] 7.4 Integration test: `DRY_RUN` generates the override file and skips terraform; assert resolved `region`/`access_key` are encoded in the override
- [ ] 7.5 Integration test: pre-existing override file causes a clear failure and is not deleted
- [ ] 7.6 Integration test: leading `--region`/`--account` are stripped from forwarded args and encoded into the override; invalid `--account` fails before terraform is invoked
- [ ] 7.7 Integration test: positional rules — `--region`/`--account` placed after the terraform action are forwarded to terraform unchanged (not consumed by lstk), and placing them before the `terraform` subcommand (`lstk --account … terraform`) is rejected
- [ ] 7.8 Integration test (Docker-gated): override file is generated and removed after a real `terraform plan` against a running LocalStack, with endpoint keys sourced from the provider schema
- [ ] 7.9 Integration test: when the provider schema is unavailable (no `terraform init` / provider not installed), the command fails with the specific "run `terraform init` first" message and does not invoke terraform or generate an override

## 8. Documentation

- [ ] 8.1 Ensure command `Long` help lists `--region`/`--account` and supported env vars, documents that LocalStack must be running, and notes that `terraform init` must have been run (so the provider schema is available)
