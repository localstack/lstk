## Context

lstk already proxies the AWS CLI through `lstk aws` ([cmd/aws.go](../../../cmd/aws.go), [internal/awscli/exec.go](../../../internal/awscli/exec.go)). That command: strips lstk-specific flags, creates a Docker runtime handle, resolves the AWS emulator `ContainerConfig`, checks runtime health, resolves the running container by name/image via `container.ResolveRunningContainerName`, resolves `host:port` via `endpoint.ResolveHost`, and finally execs the `aws` binary with stdin/stdout/stderr passed through and `AWS_*` env vars defaulted.

We want the same ergonomics for Terraform. The reference behavior is LocalStack's Python `tflocal` wrapper, which generates a Terraform *override file* (`localstack_providers_override.tf`) so the AWS provider's `endpoints {}` block points at LocalStack, runs the real `terraform`, then deletes the file. Terraform automatically merges any file named `*_override.tf` over the user's own config, so this requires no edits to user files.

Two deliberate departures from `tflocal` are required by this change:
1. The endpoint key list must be discovered dynamically from `terraform providers schema -json` instead of `tflocal`'s hand-maintained service tables.
2. A reduced environment-variable surface (see Decisions), since lstk resolves the endpoint itself.

`tflocal` reference behavior (verified from the current `bin/tflocal` source) that we carry over: generate one `provider "aws"` block per discovered provider/alias; set `access_key`/`secret_key = "test"`, `skip_credentials_validation`, `skip_metadata_api_check`, region, and conditional `s3_use_path_style`; skip override generation for `fmt`/`validate`/`version`; honor a dry-run mode (`LSTK_TF_DRY_RUN`); clean up generated files in a deferred step.

## Goals / Non-Goals

**Goals:**
- `lstk terraform` / `lstk tf` proxies all args to `terraform`, streaming I/O and propagating exit code.
- Auto-resolve the LocalStack endpoint from the running emulator — no host/port env vars needed.
- Generate the AWS provider override file with endpoint keys discovered from the provider schema, then clean it up.
- Support `--region` and `--account` lstk-specific flags that select the deployment region and target account, encoded into the generated override block.
- Support `AWS_ENDPOINT_URL`, `LSTK_TF_CMD`, `LSTK_TF_OVERRIDE_FILE_NAME`, `LSTK_TF_DRY_RUN`, `AWS_REGION`, and `AWS_ACCESS_KEY_ID`. lstk-invented variables are prefixed `LSTK_TF_` to avoid clashing with generic names already present in users' environments (e.g. a bare `DRY_RUN`).
- Keep the architecture aligned with lstk conventions: cmd/ wiring only, domain logic in `internal/iac/terraform/cli/`, output via `output.Sink`, diagnostics via `log.Logger`, no direct stdout/stderr prints, no `config.Get()` in domain code.

**Non-Goals:**
- Porting `tflocal`'s S3 backend / `terraform_remote_state` support (eager state-bucket and DynamoDB lock-table creation). Out of scope for the first version; can be a follow-up.
- Supporting the dropped env vars: `LOCALSTACK_HOSTNAME`, `EDGE_PORT`, `USE_LEGACY_PORTS`, `S3_HOSTNAME`, `USE_EXEC`, `CUSTOMIZE_ACCESS_KEY`, `AWS_ACCESS_KEY_ID`-based key derivation, `ADDITIONAL_TF_OVERRIDE_LOCATIONS`, `TF_UNPROXIED_CMDS`.
- A spinner / progress UI around terraform output.
- Pulling real credentials from the ambient AWS credential chain (`CUSTOMIZE_ACCESS_KEY`-style behavior). The only supported account control is the explicit `--account` flag / `AWS_ACCESS_KEY_ID` value; `secret_key` is always `"test"`. (Note: we still carry over `tflocal`'s `deactivate_access_key` safeguard for the `AWS_ACCESS_KEY_ID` value — see "Region and account selection" — so a real key in the env is neutralized rather than used.)

## Decisions

### Command wiring mirrors `lstk aws`
`cmd/terraform.go` defines a Cobra command with `Use: "terraform [args...]"`, `Aliases: []string{"tf"}`, `DisableFlagParsing: true`, and `PreRunE: initConfig(nil)`. It reuses the exact preamble from `aws.go`: `stripNonInteractiveFlag`, `runtime.NewDockerRuntime`, resolve the `config.EmulatorAWS` `ContainerConfig`, `rt.IsHealthy`, `container.ResolveRunningContainerName` (emit the same "not running" `ErrorEvent` when empty), and `endpoint.ResolveHost`. In addition it strips and validates the lstk-specific `--region`/`--account` flags (see "Region and account selection") at the boundary, resolving the effective region and account before handing off. The resolved `http://host:port`, region, account, plus a `*output.Sink` and a `log.Logger` are handed to a new domain entry point `tfcli.Run(...)`.

**Alternative considered:** generalize `lstk aws` into a shared proxy abstraction. Rejected for now — the override-file generation makes terraform materially different from a pure CLI passthrough, and premature generalization would obscure both. We reuse the discrete helpers (`ResolveRunningContainerName`, `ResolveHost`) instead of the whole command body.

**AWS emulator only.** `lstk terraform` always targets the AWS emulator (it resolves `config.EmulatorAWS`), and discovery — by name (`localstack-aws`) then AWS image repos on the edge port — never matches a Snowflake/Azure container, so a non-AWS emulator can't be picked up by accident. The gap is only the message: when the AWS emulator isn't found, rather than the generic "not running" error, the command checks `container.RunningEmulators(...)` across the known emulator types and, if a non-AWS one is running, fails with an AWS-specific error naming it (so a user who started Snowflake/Azure and expected terraform to work gets a clear explanation rather than a misleading "AWS not running").

### New domain package `internal/iac/terraform/cli/`
Code lives under an `internal/iac/` umbrella to reserve room for future infrastructure-as-code tooling (e.g. `internal/iac/pulumi/`, `internal/iac/cdk/`) and future non-CLI Terraform code (`internal/iac/terraform/…`), mirroring how `internal/runtime/` abstracts container runtimes. The CLI wrapper for this change is the leaf package `internal/iac/terraform/cli` (Go `package cli`), imported at the single call site with the alias `tfcli`:
```go
import tfcli "github.com/localstack/lstk/internal/iac/terraform/cli"
```
No shared `iac` interface is introduced yet (YAGNI) — the directory layout alone reserves the abstraction seam. All business logic lives in this package (no logic in cmd/). Suggested decomposition:
- `exec.go` — `Run(ctx, endpointURL, region, account string, sink output.Sink, logger log.Logger, args []string) error`: orchestrates binary lookup, unproxied/dry-run detection, override generation + deferred cleanup, and subprocess execution. Locates the binary via `exec.LookPath(tfCmd())` where `tfCmd()` reads `LSTK_TF_CMD` (default `terraform`). Subprocess uses `exec.CommandContext` with stdin/stdout/stderr wired to the passed writers and `os.Stdin`; non-zero exit wrapped as `output.NewSilentError` (same as awscli). No env-var injection of `AWS_*` is needed because credentials/region live in the override file.
- `schema.go` — `EndpointKeys(ctx, tfBin, workdir string) ([]string, error)`: runs `<tfBin> providers schema -json`, parses the JSON, and navigates `provider_schemas["registry.terraform.io/hashicorp/aws"].provider.block.block_types["endpoints"].block.attributes` to collect endpoint key names. There is **no fallback list**. Distinct error cases:
  - The `providers schema` invocation exits non-zero, or the AWS provider key is absent from `provider_schemas` → the provider is not installed (typically `terraform init` has not been run). Return a dedicated `ErrInitRequired` whose message instructs the user to run `terraform init` first.
  - The command succeeds and the AWS provider is present but yields no endpoint keys (unexpected/incompatible schema shape) → return a plain error so the caller fails rather than generating an empty `endpoints {}` block.
  Works for the AWS provider 4.0 and higher — every such version exposes the `endpoints` nested block under the provider schema.
- `override.go` — `GenerateOverride(opts)`: discovers `aws` provider blocks (and their `alias`/`region`) in the working directory's `*.tf` files, then renders one `provider "aws"` block per discovered alias into the override file. Renders mock creds, region, skip flags, conditional `s3_use_path_style`, and the `endpoints {}` block. Each endpoint key is written **exactly as named in the provider schema**, all mapped to the single resolved LocalStack endpoint (no per-service env overrides). Returns the list of written file paths for cleanup.
- `defaults.go` — the fixed passthrough-command set (`fmt`, `validate`, `version`, `init`). `init` is included because schema discovery requires the provider to be installed, which only happens once `init` runs; `init` is therefore run directly (no override, no emulator) to bootstrap the provider for subsequent `plan`/`apply`.

### Provider-block discovery scope
Provider blocks are discovered by **recursing the working-directory tree** (`filepath.WalkDir`) and parsing every `*.tf` file, not just the top-level ones. Idiomatic Terraform keeps provider *configuration* in the root module, but real projects don't always follow that, so scanning sub-directories represents more `aws` provider/alias blocks than a top-level-only scan would. This is explicitly a best-effort heuristic — it won't catch every layout (e.g. modules sourced remotely from a registry or git, which aren't on local disk until pulled), but it is strictly better than not recursing. The walk skips hidden directories: `.terraform` in particular holds the downloaded provider/module cache and must not be scanned (its vendored modules' provider blocks aren't the user's own config), and `.git` is irrelevant. A `*.tf` that fails to parse is logged and skipped individually rather than aborting discovery; if nothing is found, we fall back to a single default (alias-less) provider. The generated `*_override.tf` is still written once at the working-directory root. (End-to-end cases 8.2–8.3 verify the flat-project and sub-module shapes.)

When a user's own provider block explicitly sets an `endpoints {}` block (e.g. pointing at FIPS endpoints), the generated override is expected to win — Terraform merges any `*_override.tf` over the user's configuration, and our block carries the resolved LocalStack endpoints. Case 8.6 verifies this precedence against a real `terraform plan`.

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

**Strict leading-only parsing.** The parser recognizes these flags only at the head of the terraform args — i.e. between `terraform`/`tf` and the terraform action (`plan`, `apply`, …). It scans tokens from the start and stops at the first token that is not `--region`/`--account` (or the value of one); everything from that point on is forwarded verbatim. This means `lstk terraform --region us-east-2 plan` is parsed, but `lstk terraform plan --region us-east-2` forwards `--region us-east-2` to terraform untouched (terraform then rejects it). Rationale: stopping at the first non-flag token is robust against terraform argument values that happen to contain `--region`/`--account` (e.g. inside a `-var` value), and it gives a single, predictable place for these options. The flags are deliberately **not** registered as root/persistent Cobra flags. The `lstk --account … terraform` form (flag before the subcommand) cannot be reliably rejected by Cobra: because the terraform command sets `DisableFlagParsing`, Cobra consumes such flags during command resolution and silently drops them rather than erroring. To avoid a silent no-op, `RunE` inspects the raw `os.Args` and fails with a clear error when `--region`/`--account` appear before the `terraform`/`tf` token.

Resolution precedence:

| Provider attr | Source order | Default |
|---|---|---|
| `region` | `--region` → `AWS_REGION` env → default | `us-east-1` |
| `access_key` | `--account` → `AWS_ACCESS_KEY_ID` env → default | `test` |

`secret_key` is always the literal `"test"` (LocalStack ignores it). The `--account` flag value is validated against `^\d{12}$`. An `AWS_ACCESS_KEY_ID` env value is not validated (users may export a non-12-digit mock key) but is run through an **access-key deactivation** step before being encoded: if it starts with `A` (the prefix of real AWS keys — `AKIA…` long-term, `ASIA…` temporary), the leading `A` is rewritten to `L`. This carries over `tflocal`'s `deactivate_access_key` safeguard so a live credential accidentally present in the environment is never written into the override file or sent to LocalStack (where it could be picked up by analytics). The validated 12-digit flag value can never start with `A`, so it is used as-is. The deprecated `AWS_DEFAULT_REGION` is intentionally **not** read.

**Decision — encode into the override, not env passthrough.** The Terraform AWS provider resolves config in the order: explicit provider-block args → environment variables → shared config files. We encode `region`/`access_key` directly into the generated override block (precedence level 1) rather than exporting `AWS_REGION`/`AWS_ACCESS_KEY_ID` to the terraform subprocess. This makes targeting deterministic and matches `tflocal`, which also bakes these into the override. **Consequence:** the encoded values take precedence over any `region`/`access_key` the user wrote in their own provider block — the intended "deploy to *this* region/account" forcing behavior, recorded here so it is deliberate rather than surprising.

**Alternative considered:** export the values as env vars on the subprocess and omit them from the override block. Rejected — env vars sit below explicit provider-block args, so any `region`/`access_key` (including the literal `test` we'd otherwise still need for the endpoints to authenticate) in the block would silently win, making the flags appear to do nothing.

### Working directory selection (`-chdir`)
Terraform's global `-chdir=DIR` option must precede the subcommand and makes Terraform operate in `DIR` (all relative paths, including where it reads `*.tf`/`*_override.tf` and where `.terraform/` lives, become relative to `DIR`). lstk already forwards args verbatim, so `-chdir` works for **unproxied** commands with no extra code — terraform performs the switch itself and lstk touches no directory. The break is only in the **proxied** path, where lstk does three directory-relative things, all currently anchored to `os.Getwd()`: schema discovery (`EndpointKeys` runs `providers schema -json` with `cmd.Dir = workdir`), override generation + `aws`-provider-block discovery (`generateOverride`/`discoverAWSAliases` over `workdir`), and the deferred cleanup. Run with `-chdir`, these would read from and write into the wrong directory — yielding either a misleading "init required" or, worse, an override file terraform never sees, silently provisioning against real AWS.

**Decision — compute an effective workdir and thread it where `os.Getwd()` is used.** The plumbing already passes `workdir` explicitly through `EndpointKeys`/`generateOverride`, so the entire fix is to derive the effective directory once and substitute it: `workdir = resolve(getwd, DIR)` when `-chdir=DIR` is present (absolute `DIR` as-is, relative `DIR` joined to getwd). The schema probe sets `cmd.Dir = workdir` directly, so it needs no `-chdir` re-injection. The real terraform run, however, executes with no `cmd.Dir`, so `-chdir=DIR` **must remain in the forwarded args** for terraform to switch — meaning lstk *reads* `-chdir` but, unlike `--region`/`--account`, does **not** strip it.

**`-chdir=DIR` form only.** lstk recognizes only the `=` form, mirroring Terraform exactly (Terraform does not accept a space-separated `-chdir DIR`). A space form is forwarded untouched and terraform reports its own error — lstk does not invent a spelling Terraform rejects.

**Flag-parsing interaction.** `-chdir` and lstk's `--region`/`--account` are all leading flags, but of different kinds: `-chdir` is read-and-kept, the lstk flags are read-and-removed. The leading-flag scan is extended to recognize `-chdir=DIR` and **continue past it** (rather than stopping, as it would for any other non-matching token), so `--region`/`--account` on either side of `-chdir` are still consumed. A useful property falls out: because removing the lstk flags collapses `-chdir` back toward the front, the user may write the leading flags in any order and `-chdir` still lands first in the forwarded args, satisfying Terraform's "must be first" rule:
```
lstk tf --region x -chdir=infra plan   ─strip ──►  -chdir=infra plan   ✓
lstk tf -chdir=infra --region x plan   ─strip ──►  -chdir=infra plan   ✓
```

**Validate the directory up front.** Without a check, a bad `DIR` surfaces as a misleading `ErrInitRequired` from the schema probe (whose `cmd.Dir` points at a nonexistent path). lstk `os.Stat`s the effective directory before any work and fails with a clear "directory not found" error naming `DIR`, so the failure is honest rather than mislabeled.

**Alternative considered — inject `-chdir` into the schema probe's args instead of setting `cmd.Dir`.** Rejected: `cmd.Dir` is already how `EndpointKeys` scopes the probe, so resolving the effective workdir and reusing the existing parameter is smaller and keeps a single source of truth for "where are we operating".

### Endpoint URL construction & S3 path style
The base endpoint is `http://host:port` from `endpoint.ResolveHost`, overridable by `AWS_ENDPOINT_URL`. Because `S3_HOSTNAME` is dropped, every service except S3 maps to that single resolved endpoint. `s3_use_path_style` is set to `true` when the resolved host does not support virtual-host-style bucket addressing — typically `127.0.0.1`/`localhost`, where path style is required. When the resolved host is a virtual-host-capable `*.localstack.cloud` domain (e.g. `localhost.localstack.cloud`), virtual-host style works and path style is left off.

**S3 endpoint host prefix (edge case).** Virtual-host-style addressing places the bucket as a subdomain of the S3 endpoint host (`my-bucket.<s3-host>`), so the S3 endpoint host itself must carry an `s3.` prefix. The `s3` endpoint key is therefore special-cased, gated by the same condition that drives path style:

| Resolved host | `s3_use_path_style` | `s3` endpoint value | other services |
|---|---|---|---|
| virtual-host-capable `*.localstack.cloud` (e.g. `localhost.localstack.cloud`) | `false` | `scheme://s3.<host>:<port>` | `scheme://<host>:<port>` |
| `127.0.0.1` / `localhost` (path style) | `true` | `scheme://<host>:<port>` (no prefix) | `scheme://<host>:<port>` |

The prefix is applied only when the host does not already begin with `s3.`, to avoid double-prefixing. This matches `tflocal`'s default of `s3.localhost.localstack.cloud`. The prefix logic keys off the host alone; an explicit `AWS_ENDPOINT_URL` is parsed for its host and treated the same way.

### Reduced environment-variable surface
Supported: `AWS_ENDPOINT_URL`, `LSTK_TF_CMD`, `LSTK_TF_OVERRIDE_FILE_NAME`, `LSTK_TF_DRY_RUN`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`. (`AWS_REGION` and `AWS_ACCESS_KEY_ID` act as the env-fallback sources for the `--region`/`--account` flags — see "Region and account selection".)

**lstk-invented variables are prefixed `LSTK_TF_`.** The variables that are lstk's own invention (not AWS or Terraform standards) carry an `LSTK_TF_` prefix so they can't collide with unrelated values already exported in a user's shell — a bare `DRY_RUN` in particular is generic enough to already be set for some other tool. This is a deliberate departure from `tflocal`'s names (`TF_CMD`, `LS_PROVIDERS_FILE`, `DRY_RUN`), prioritizing consistency and clarity of the new experience over parity with the old CLI. The mapping is `TF_CMD` → `LSTK_TF_CMD`, `LS_PROVIDERS_FILE` → `LSTK_TF_OVERRIDE_FILE_NAME`, `DRY_RUN` → `LSTK_TF_DRY_RUN`. `AWS_ENDPOINT_URL`, `AWS_REGION`, and `AWS_ACCESS_KEY_ID` keep their standard AWS names.

Dropped (per maintainer decision): `SKIP_ALIASES` (`tflocal`'s per-alias skip list — removed entirely as it provides no clear value for lstk; an override block is generated for every discovered `aws` provider/alias), `AWS_DEFAULT_REGION` (deprecated — use `AWS_REGION`), `LOCALSTACK_HOSTNAME`, `EDGE_PORT` (lstk resolves host:port), `USE_LEGACY_PORTS` (single-port LocalStack), `S3_HOSTNAME` (path style auto-decided), `USE_EXEC` (breaks cleanup — we always use a managed subprocess so the deferred cleanup runs), `CUSTOMIZE_ACCESS_KEY` (no ambient-credential-chain derivation; account is set only via `--account` / `AWS_ACCESS_KEY_ID`), `ADDITIONAL_TF_OVERRIDE_LOCATIONS` (single working-dir override), `TF_UNPROXIED_CMDS` (fixed set), and per-service `<SERVICE>_ENDPOINT` overrides (endpoint keys come straight from the schema and all point at the single resolved LocalStack endpoint, so there is nothing to override per-service). Reading the supported env vars stays at the domain boundary inside `internal/iac/terraform/cli` (they are process env, not lstk `config.Get()` values), consistent with the project rule that domain code must not call `config.Get()`.

### No spinner; subprocess (not exec-replace)
Per the requirement, no spinner wraps terraform. Unlike `aws.go`, we do **not** wrap stdout/stderr in `StopOnWriteWriter`. We still run terraform as a managed child process (`exec.CommandContext`) rather than `syscall.Exec`, because the deferred override-file cleanup must run after terraform exits — `USE_EXEC`-style process replacement would skip it.

### Override-file safety
Before writing, check whether the target override file already exists. If it does, fail with a clear error and let the user resolve the conflict. lstk keeps **no persistent record** of whether it created the file, so it cannot (and does not try to) distinguish a user-authored file from one it left behind itself. An existing file is therefore always one of two things: authored manually (by the user or a teammate), or orphaned by a previous `lstk tf` run that was interrupted before its deferred cleanup ran. In either case, failing and asking the user to remove the file (or point `LSTK_TF_OVERRIDE_FILE_NAME` elsewhere) is the safe choice — it guarantees cleanup never deletes a file lstk did not write in the current run. Cleanup is a `defer` that removes only the paths lstk wrote during this invocation.

(This is a deliberate simplification over an earlier idea of tagging generated files with a marker comment and silently overwriting "our own" leftovers: a marker is not a reliable record of authorship — a user could copy or hand-edit such a file — and silently clobbering it risks data loss. Failing loudly is preferred.)

### End-to-end test strategy
Section 7's tests use a stub `terraform` and an alpine stand-in for the emulator (fast, no downloads). Section 8 adds full end-to-end tests (`test/integration/terraform_e2e_test.go`) wired into the default CI integration run, with three implementation decisions worth recording:

- **Real emulator = `localstack/localstack-pro` + token.** lstk's default AWS emulator is the pro image, which activates against `LOCALSTACK_AUTH_TOKEN`. The bring-up helpers live in `test/integration/localstack_test.go` (`startRealLocalStack`/`waitForLocalStackReady`/`requireAuthToken`) — kept separate from the terraform test file, and `startRealLocalStack` is parameterized by image + container name, so future e2e suites for the Snowflake/Azure emulators can reuse it rather than duplicating the logic (the edge port 4566 and `/_localstack/health` are uniform across emulators). The harness starts that image named `localstack-aws` (so lstk's name-based discovery finds it) with `4566` bound to `127.0.0.1` — the address `endpoint.ResolveHost` resolves to — so the host-side `terraform` subprocess can reach it. The token is the same CI secret the rest of the suite already uses; tests skip when it (or a `terraform`/`tofu` binary) is absent.
- **`apply`, not bare `plan`.** Although the matrix was framed around `terraform plan`, a create-only plan against empty state makes no API calls (and the provider skips credential/metadata checks), so it never contacts LocalStack and can't validate the override. Routing-sensitive cases run `terraform apply -auto-approve` of a single S3 bucket; a successful apply proves `CreateBucket` was routed through the override to LocalStack. The FIPS case (8.6) doubles as the strongest proof: apply succeeds only if our override beat the user's explicit public FIPS endpoint.
- **CI installs the binaries.** The Linux shards install Terraform and OpenTofu (`hashicorp/setup-terraform` / `opentofu/setup-opentofu`, wrapper disabled) so these run in CI; macOS/Windows shards skip. The integration timeout was raised (15m → 25m) to absorb the image pull, provider downloads, and LocalStack boots.
- **Samples are files, not heredocs.** Each project lives under `test/integration/test-samples/iac/terraform/<name>/` as real, readable, hand-runnable Terraform; the test copies it into a temp dir (`copySample` via `os.CopyFS`) so terraform's `.terraform`/state and lstk's override never touch the tracked tree (those artifacts are also gitignored under `test-samples/`). The `.terraform` decoy for the recursion test is created at runtime rather than committed, since a fake cache dir in the repo would be confusing.

## Risks / Trade-offs

- **HCL parsing of user `.tf` files** → Discovering provider blocks/aliases requires parsing HCL. Mitigation: use the HashiCorp HCL Go library (`github.com/hashicorp/hcl/v2` / `hclparse`), idiomatic in the Terraform ecosystem; on parse failure, fall back to generating a single default (alias-less) provider override and log the degradation. Adding this dependency is accepted.
- **Schema query latency** → `terraform providers schema -json` spawns a process and can be slow on large configs. Mitigation: only invoked for proxied commands (not `fmt`/`validate`/`version`/`init`).
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

- **Mutually-exclusive endpoint keys** — RESOLVED. Some endpoint keys are aliases for the same service and are mutually exclusive; setting several in one `endpoints {}` block makes the provider emit "Invalid Attribute Combination — Only one of the following attributes should be set …" ("will be an error in a future release"). Two facts shaped the fix: (1) `terraform providers schema -json` carries **no** alias/conflict metadata (all endpoint attributes are identical `{type, optional, description}`, no `deprecated`), so the filter **cannot** be schema-driven and needs a maintained alias-group table; (2) the warning surfaces only on **`terraform plan` once a resource forces provider configuration** — *not* on `validate`, and *not* on a plan with no resources (which is why an earlier `validate`-only probe wrongly concluded "no conflict"). The authoritative group set was captured from `terraform plan -json` on AWS provider 6.48 (39 groups, including both `databrew`/`gluedatabrew` and the `lexmodels`/`lexmodelbuilding`/`lexmodelbuildingservice`/`lex` group), unioned with `simpledb`/`sdb` for older providers. `dedupeAliasKeys` (schema.go) keeps the first present member of each group and drops the rest — all members are aliases for the same endpoint, so any one routes the service to LocalStack. Verified end-to-end: the deduped set (313→265 keys) produces **zero** conflict warnings on 6.48. The table is provider-version-sensitive and maintained by hand (the schema can't supply it); this is a deliberate, scoped exception to the "no hardcoded list" principle — key *discovery* stays dynamic; only *conflict resolution* is tabular.
