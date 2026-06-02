## Context

lstk already proxies the AWS CLI through `lstk aws` ([cmd/aws.go](../../../cmd/aws.go), [internal/awscli/exec.go](../../../internal/awscli/exec.go)). That command: strips lstk-specific flags, creates a Docker runtime handle, resolves the AWS emulator `ContainerConfig`, checks runtime health, resolves the running container by name/image via `container.ResolveRunningContainerName`, resolves `host:port` via `endpoint.ResolveHost`, and finally execs the `aws` binary with stdin/stdout/stderr passed through and `AWS_*` env vars defaulted.

We want the same ergonomics for Terraform. The reference behavior is LocalStack's Python `tflocal` wrapper, which generates a Terraform *override file* (`localstack_providers_override.tf`) so the AWS provider's `endpoints {}` block points at LocalStack, runs the real `terraform`, then deletes the file. Terraform automatically merges any file named `*_override.tf` over the user's own config, so this requires no edits to user files.

Two deliberate departures from `tflocal` are required by this change:
1. The endpoint key list must be discovered dynamically from `terraform providers schema -json` instead of `tflocal`'s hand-maintained service tables.
2. A reduced environment-variable surface (see Decisions), since lstk resolves the endpoint itself.

`tflocal` reference behavior (verified from the current `bin/tflocal` source) that we carry over: generate one `provider "aws"` block per discovered provider/alias; set `access_key`/`secret_key = "test"`, `skip_credentials_validation`, `skip_metadata_api_check`, region, and conditional `s3_use_path_style`; skip override generation for `fmt`/`validate`/`version`; honor `DRY_RUN`; clean up generated files in a deferred step.

## Goals / Non-Goals

**Goals:**
- `lstk terraform` / `lstk tf` proxies all args to `terraform`, streaming I/O and propagating exit code.
- Auto-resolve the LocalStack endpoint from the running emulator — no host/port env vars needed.
- Generate the AWS provider override file with endpoint keys discovered from the provider schema, then clean it up.
- Support `--region` and `--account` lstk-specific flags that select the deployment region and target account, encoded into the generated override block.
- Support `AWS_ENDPOINT_URL`, `TF_CMD`, `LS_PROVIDERS_FILE`, `DRY_RUN`, `SKIP_ALIASES`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`, and `<SERVICE>_ENDPOINT` overrides.
- Keep the architecture aligned with lstk conventions: cmd/ wiring only, domain logic in `internal/iac/terraform/cli/`, output via `output.Sink`, diagnostics via `log.Logger`, no direct stdout/stderr prints, no `config.Get()` in domain code.

**Non-Goals:**
- Porting `tflocal`'s S3 backend / `terraform_remote_state` support (eager state-bucket and DynamoDB lock-table creation). Out of scope for the first version; can be a follow-up.
- Supporting the dropped env vars: `LOCALSTACK_HOSTNAME`, `EDGE_PORT`, `USE_LEGACY_PORTS`, `S3_HOSTNAME`, `USE_EXEC`, `CUSTOMIZE_ACCESS_KEY`, `AWS_ACCESS_KEY_ID`-based key derivation, `ADDITIONAL_TF_OVERRIDE_LOCATIONS`, `TF_UNPROXIED_CMDS`.
- A spinner / progress UI around terraform output.
- Pulling real credentials from the ambient AWS credential chain (`CUSTOMIZE_ACCESS_KEY`-style behavior). The only supported account control is the explicit `--account` flag / `AWS_ACCESS_KEY_ID` value; `secret_key` is always `"test"`.

## Decisions

### Command wiring mirrors `lstk aws`
`cmd/terraform.go` defines a Cobra command with `Use: "terraform [args...]"`, `Aliases: []string{"tf"}`, `DisableFlagParsing: true`, and `PreRunE: initConfig(nil)`. It reuses the exact preamble from `aws.go`: `stripNonInteractiveFlag`, `runtime.NewDockerRuntime`, resolve the `config.EmulatorAWS` `ContainerConfig`, `rt.IsHealthy`, `container.ResolveRunningContainerName` (emit the same "not running" `ErrorEvent` when empty), and `endpoint.ResolveHost`. In addition it strips and validates the lstk-specific `--region`/`--account` flags (see "Region and account selection") at the boundary, resolving the effective region and account before handing off. The resolved `http://host:port`, region, account, plus a `*output.Sink` and a `log.Logger` are handed to a new domain entry point `tfcli.Run(...)`.

**Alternative considered:** generalize `lstk aws` into a shared proxy abstraction. Rejected for now — the override-file generation makes terraform materially different from a pure CLI passthrough, and premature generalization would obscure both. We reuse the discrete helpers (`ResolveRunningContainerName`, `ResolveHost`) instead of the whole command body.

### New domain package `internal/iac/terraform/cli/`
Code lives under an `internal/iac/` umbrella to reserve room for future infrastructure-as-code tooling (e.g. `internal/iac/pulumi/`, `internal/iac/cdk/`) and future non-CLI Terraform code (`internal/iac/terraform/…`), mirroring how `internal/runtime/` abstracts container runtimes. The CLI wrapper for this change is the leaf package `internal/iac/terraform/cli` (Go `package cli`), imported at the single call site with the alias `tfcli`:
```go
import tfcli "github.com/localstack/lstk/internal/iac/terraform/cli"
```
No shared `iac` interface is introduced yet (YAGNI) — the directory layout alone reserves the abstraction seam. All business logic lives in this package (no logic in cmd/). Suggested decomposition:
- `exec.go` — `Run(ctx, endpointURL, region, account string, sink output.Sink, logger log.Logger, args []string) error`: orchestrates binary lookup, unproxied/dry-run detection, override generation + deferred cleanup, and subprocess execution. Locates the binary via `exec.LookPath(tfCmd())` where `tfCmd()` reads `TF_CMD` (default `terraform`). Subprocess uses `exec.CommandContext` with stdin/stdout/stderr wired to the passed writers and `os.Stdin`; non-zero exit wrapped as `output.NewSilentError` (same as awscli). No env-var injection of `AWS_*` is needed because credentials/region live in the override file.
- `schema.go` — `EndpointKeys(ctx, tfBin, workdir string) ([]string, error)`: runs `<tfBin> providers schema -json`, parses the JSON, and navigates `provider_schemas["registry.terraform.io/hashicorp/aws"].provider.block.block_types["endpoints"].block.attributes` to collect endpoint key names. There is **no fallback list**. Distinct error cases:
  - The `providers schema` invocation exits non-zero, or the AWS provider key is absent from `provider_schemas` → the provider is not installed (typically `terraform init` has not been run). Return a dedicated `ErrInitRequired` whose message instructs the user to run `terraform init` first.
  - The command succeeds and the AWS provider is present but yields no endpoint keys (unexpected/incompatible schema shape) → return a plain error so the caller fails rather than generating an empty `endpoints {}` block.
  Works for the AWS provider 4.0 and higher — every such version exposes the `endpoints` nested block under the provider schema.
- `override.go` — `GenerateOverride(opts)`: discovers `aws` provider blocks (and their `alias`/`region`) in the working directory's `*.tf` files, then renders one `provider "aws"` block per non-skipped alias into the override file. Renders mock creds, region, skip flags, conditional `s3_use_path_style`, and the `endpoints {}` block. Each endpoint key is written **exactly as named in the provider schema**, all mapped to the single resolved LocalStack endpoint (no per-service env overrides). Returns the list of written file paths for cleanup.
- `defaults.go` — the fixed unproxied-command set (`fmt`, `validate`, `version`).

### Dynamic endpoint discovery via `terraform providers schema -json`
The endpoint keys are read from the provider schema rather than a static table. This auto-adapts to AWS provider major-version differences (v4 vs v5 vs v6 endpoint keys) and removes `tflocal`'s three hand-maintained tables (`SERVICE_EXCLUSIONS`, `SERVICE_REPLACEMENTS`, `SERVICE_ALIASES`). It is supported for the AWS provider **4.0 and higher**. The schema JSON shape relied upon:
```
provider_schemas
  └─ "registry.terraform.io/hashicorp/aws"
       └─ provider.block.block_types.endpoints.block.attributes  ← keys = endpoint names
```
Each discovered key is written into the `endpoints {}` block verbatim (no case transformation) — the keys are consumed by Terraform exactly as the schema reports them.

**No fallback list.** Unlike `tflocal`, we do not ship a static endpoint table. If discovery yields no keys the command **fails** rather than guessing. The dominant cause of an unavailable schema is that `terraform init` has not yet installed the AWS provider; that case is detected (non-zero `providers schema` exit, or the AWS provider absent from `provider_schemas`) and surfaced as a specific "run `terraform init` first" error. Rejected alternative — shipping a static list as a pre-`init` fallback: it requires ongoing maintenance, drifts across provider versions, and silently masks the real problem (the user needs to `init`). Failing loudly with actionable guidance is preferred.

### Region and account selection (`--region` / `--account`)
`--region` and `--account` are lstk-specific, not Terraform flags. Because the command uses `DisableFlagParsing: true`, they are extracted with a small boundary-level parser (generalizing `stripNonInteractiveFlag`) that handles both `--flag value` and `--flag=value`, removes them and their values from the forwarded args, and errors clearly when a value is missing. The resolved values are passed into `tfcli.Run` and ultimately encoded by `GenerateOverride` into each provider block.

**Strict leading-only parsing.** The parser recognizes these flags only at the head of the terraform args — i.e. between `terraform`/`tf` and the terraform action (`plan`, `apply`, …). It scans tokens from the start and stops at the first token that is not `--region`/`--account` (or the value of one); everything from that point on is forwarded verbatim. This means `lstk terraform --region us-east-2 plan` is parsed, but `lstk terraform plan --region us-east-2` forwards `--region us-east-2` to terraform untouched (terraform then rejects it). Rationale: stopping at the first non-flag token is robust against terraform argument values that happen to contain `--region`/`--account` (e.g. inside a `-var` value), and it gives a single, predictable place for these options. The flags are deliberately **not** registered as root/persistent Cobra flags, so the `lstk --account … terraform` form is rejected by Cobra's normal unknown-flag handling — no special-casing needed.

Resolution precedence:

| Provider attr | Source order | Default |
|---|---|---|
| `region` | `--region` → `AWS_REGION` env → default | `us-east-1` |
| `access_key` | `--account` → `AWS_ACCESS_KEY_ID` env → default | `test` |

`secret_key` is always the literal `"test"` (LocalStack ignores it). The `--account` flag value is validated against `^\d{12}$`; an `AWS_ACCESS_KEY_ID` env value is forwarded verbatim (unvalidated) since users may already export a non-12-digit mock key. The deprecated `AWS_DEFAULT_REGION` is intentionally **not** read.

**Decision — encode into the override, not env passthrough.** The Terraform AWS provider resolves config in the order: explicit provider-block args → environment variables → shared config files. We encode `region`/`access_key` directly into the generated override block (precedence level 1) rather than exporting `AWS_REGION`/`AWS_ACCESS_KEY_ID` to the terraform subprocess. This makes targeting deterministic and matches `tflocal`, which also bakes these into the override. **Consequence:** the encoded values take precedence over any `region`/`access_key` the user wrote in their own provider block — the intended "deploy to *this* region/account" forcing behavior, recorded here so it is deliberate rather than surprising.

**Alternative considered:** export the values as env vars on the subprocess and omit them from the override block. Rejected — env vars sit below explicit provider-block args, so any `region`/`access_key` (including the literal `test` we'd otherwise still need for the endpoints to authenticate) in the block would silently win, making the flags appear to do nothing.

### Endpoint URL construction & S3 path style
The base endpoint is `http://host:port` from `endpoint.ResolveHost`, overridable by `AWS_ENDPOINT_URL`. Because `S3_HOSTNAME` is dropped, every service except S3 maps to that single resolved endpoint. `s3_use_path_style` is set to `true` when the resolved host does not support virtual-host-style bucket addressing — typically `127.0.0.1`/`localhost`, where path style is required. When the resolved host is a virtual-host-capable `*.localstack.cloud` domain (e.g. `localhost.localstack.cloud`), virtual-host style works and path style is left off.

**S3 endpoint host prefix (edge case).** Virtual-host-style addressing places the bucket as a subdomain of the S3 endpoint host (`my-bucket.<s3-host>`), so the S3 endpoint host itself must carry an `s3.` prefix. The `s3` endpoint key is therefore special-cased, gated by the same condition that drives path style:

| Resolved host | `s3_use_path_style` | `s3` endpoint value | other services |
|---|---|---|---|
| virtual-host-capable `*.localstack.cloud` (e.g. `localhost.localstack.cloud`) | `false` | `scheme://s3.<host>:<port>` | `scheme://<host>:<port>` |
| `127.0.0.1` / `localhost` (path style) | `true` | `scheme://<host>:<port>` (no prefix) | `scheme://<host>:<port>` |

The prefix is applied only when the host does not already begin with `s3.`, to avoid double-prefixing. This matches `tflocal`'s default of `s3.localhost.localstack.cloud`. The prefix logic keys off the host alone; an explicit `AWS_ENDPOINT_URL` is parsed for its host and treated the same way.

### Reduced environment-variable surface
Supported (carried from `tflocal`): `AWS_ENDPOINT_URL`, `TF_CMD`, `LS_PROVIDERS_FILE`, `DRY_RUN`, `SKIP_ALIASES`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`. (`AWS_REGION` and `AWS_ACCESS_KEY_ID` act as the env-fallback sources for the `--region`/`--account` flags — see "Region and account selection".)
Dropped (per maintainer decision): `AWS_DEFAULT_REGION` (deprecated — use `AWS_REGION`), `LOCALSTACK_HOSTNAME`, `EDGE_PORT` (lstk resolves host:port), `USE_LEGACY_PORTS` (single-port LocalStack), `S3_HOSTNAME` (path style auto-decided), `USE_EXEC` (breaks cleanup — we always use a managed subprocess so the deferred cleanup runs), `CUSTOMIZE_ACCESS_KEY` (no ambient-credential-chain derivation; account is set only via `--account` / `AWS_ACCESS_KEY_ID`), `ADDITIONAL_TF_OVERRIDE_LOCATIONS` (single working-dir override), `TF_UNPROXIED_CMDS` (fixed set), and per-service `<SERVICE>_ENDPOINT` overrides (endpoint keys come straight from the schema and all point at the single resolved LocalStack endpoint, so there is nothing to override per-service). Reading these env vars stays at the domain boundary inside `internal/iac/terraform/cli` (they are process env, not lstk `config.Get()` values), consistent with the project rule that domain code must not call `config.Get()`.

### No spinner; subprocess (not exec-replace)
Per the requirement, no spinner wraps terraform. Unlike `aws.go`, we do **not** wrap stdout/stderr in `StopOnWriteWriter`. We still run terraform as a managed child process (`exec.CommandContext`) rather than `syscall.Exec`, because the deferred override-file cleanup must run after terraform exits — `USE_EXEC`-style process replacement would skip it.

### Override-file safety
Before writing, check whether the target override file already exists. If it does and lstk did not create it, fail with a clear error (mirrors `tflocal`'s `check_override_file`) so we never delete a user's file during cleanup. Cleanup is a `defer` that removes only the paths lstk wrote.

## Risks / Trade-offs

- **HCL parsing of user `.tf` files** → Discovering provider blocks/aliases requires parsing HCL. Mitigation: use the HashiCorp HCL Go library (`github.com/hashicorp/hcl/v2` / `hclparse`), idiomatic in the Terraform ecosystem; on parse failure, fall back to generating a single default (alias-less) provider override and log the degradation. Adding this dependency is accepted.
- **Schema query latency** → `terraform providers schema -json` spawns a process and can be slow on large configs. Mitigation: only invoked for proxied commands (not `fmt`/`validate`/`version`).
- **Provider not installed pre-`init`** → Schema discovery returns nothing before `terraform init`. Because there is no fallback list, the command fails fast with a specific "run `terraform init` first" error rather than producing an override with no endpoints. This is the intended behavior: the user must `init` before lstk can discover the provider's endpoint keys.
- **Cleanup on hard kill** → If lstk is `SIGKILL`ed, the deferred cleanup won't run and the override file is left behind. Mitigation: the pre-existing-file safety check makes a leftover file surface clearly on the next run; document the file name. (Same residual risk `tflocal` carries under `USE_EXEC`.)
- **Endpoint-key drift between LocalStack-supported services and provider schema keys** → The schema may expose endpoint keys for services LocalStack doesn't emulate. Mitigation: harmless — Terraform accepts any endpoint key; unused entries simply never receive traffic. Avoids the maintenance burden of intersecting with a LocalStack service list.

## Migration Plan

Additive change — no migration for existing lstk users. Users currently using `tflocal` can switch to `lstk terraform`; the dropped env vars are documented in the command help. No rollback concerns beyond not shipping the command.

## Resolved Questions

- **HCL parsing dependency**: **Resolved** — adding `github.com/hashicorp/hcl/v2` is acceptable. We use the HCL library for correct provider-block/alias discovery.
- **Per-service override casing**: **Resolved by removal** — per-service `<SERVICE>_ENDPOINT` overrides are not supported. Endpoint keys are written into the override file exactly as named in the provider schema, so there is no env-var-to-schema-key mapping (and thus no casing rule) to define.

## Open Questions

- **`terraform_remote_state` / S3 backend support**: explicitly deferred (Non-Goal) — confirm that's acceptable for v1, or whether minimal backend endpoint override is needed.
