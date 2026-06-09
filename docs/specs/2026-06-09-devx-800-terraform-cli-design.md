# `lstk terraform` — proxy Terraform against a running LocalStack

**Linear:** DEVX-800 · **Branch:** `devx-800-terraform-cli` · **Scope:** new `cmd/terraform.go` + new domain package `internal/iac/terraform/cli/` + unit/integration/e2e tests. New top-level command surface. · **Status:** design (2026-06-09)

## Problem

Running Terraform against LocalStack today means installing LocalStack's separate Python wrapper `tflocal`, which generates a Terraform override file pointing the AWS provider's `endpoints {}` at LocalStack, runs `terraform`, then deletes the file. lstk already proxies the AWS CLI via `lstk aws`; users expect the same one-binary ergonomics for Terraform: `lstk terraform` / `lstk tf`, with the endpoint resolved automatically from the running emulator and no host/port env vars.

The mechanism we carry over from `tflocal` (verified against `bin/tflocal`): one `provider "aws"` block per discovered provider/alias; `access_key`/`secret_key = "test"`, `skip_credentials_validation`, `skip_metadata_api_check`, region, conditional `s3_use_path_style`; skip override for `fmt`/`validate`/`version`; honor dry-run; clean up in a deferred step. Terraform auto-merges any `*_override.tf` over the user's config, so this needs **no edits to user files**.

Two deliberate departures from `tflocal`: **(1)** discover the endpoint-key set dynamically from `terraform providers schema -json` instead of `tflocal`'s hand-maintained service tables; **(2)** a reduced env-var surface, since lstk resolves the endpoint itself.

## Critical pre-design findings (read before implementing)

These are the four things that will silently break a naive implementation. The first is the load-bearing risk, so it is stated first.

### Finding 1 — Endpoint keys are mutually exclusive, and the schema won't tell you which (load-bearing)
Some endpoint keys are aliases for the same service (`databrew`/`gluedatabrew`; `lexmodels`/`lexmodelbuilding`/`lexmodelbuildingservice`/`lex`; `simpledb`/`sdb`; …). Writing several members of one group into a single `endpoints {}` block makes the provider emit **"Invalid Attribute Combination — Only one of the following attributes should be set"** ("will be an error in a future release"). Two facts make this nasty:
- **The schema carries no conflict metadata.** Every endpoint attribute is an identical `{type, optional, description}` with no `deprecated` marker. So the dedup **cannot** be schema-driven — it needs a **hand-maintained alias-group table**. This is a deliberate, scoped exception to the "no hardcoded list" principle: key *discovery* stays dynamic; only *conflict resolution* is tabular.
- **The warning only surfaces on `terraform plan` once a resource forces provider configuration** — not on `validate`, and not on an empty plan. (An earlier `validate`-only probe wrongly concluded "no conflict" — see the review notes below.) The authoritative group set was captured from `terraform plan -json` on AWS provider **6.48** (39 groups) ∪ `simpledb`/`sdb` for older providers. `dedupeAliasKeys` keeps the first present member of each group; verified 313→265 keys, **zero** warnings.

### Finding 2 — Routing tests must `apply`, not `plan`
A create-only `terraform plan` against empty state makes **no API calls** (the provider skips credential/metadata checks), so it never contacts LocalStack and **cannot validate the override**. Routing-sensitive e2e cases must `terraform apply -auto-approve` something real (a single S3 bucket): a successful apply proves `CreateBucket` was routed through the override to LocalStack. Discovery/precedence assertions that don't need a live call use an `LSTK_TF_DRY_RUN` capture of the override file.

### Finding 3 — No provider schema before `terraform init`, and no fallback list
Schema discovery needs the AWS provider installed, which only happens after `terraform init`. We ship **no static fallback table** (unlike `tflocal`). If the schema is unavailable (non-zero `providers schema` exit, or the AWS provider absent from `provider_schemas`) → return a dedicated `ErrInitRequired` telling the user to run `terraform init` first. If the schema is present but yields zero keys → plain error. Rationale: a static list drifts across provider versions and silently masks the real problem (the user needs to `init`). Fail loudly with actionable guidance.

### Finding 4 — lstk can't tell its own leftover override from a user's file
lstk keeps **no persistent record** of whether it wrote the override file. So a pre-existing override is *either* user-authored *or* orphaned by a prior interrupted run — indistinguishable. **Fail loudly** if the target file exists (tell the user to remove it or point `LSTK_TF_OVERRIDE_FILE_NAME` elsewhere); cleanup (a `defer`) removes **only** the paths lstk wrote during this invocation. (Rejected: tag generated files with a marker comment and silently overwrite "our own" — a marker is not reliable authorship and clobbering risks data loss.)

## Decisions

- **Wiring mirrors `lstk aws`.** `cmd/terraform.go`: `Use: "terraform [args...]"`, `Aliases: ["tf"]`, `DisableFlagParsing: true`, `PreRunE: initConfig(nil)`. Reuse the `aws.go` preamble verbatim (`stripNonInteractiveFlag`, `runtime.NewDockerRuntime`, resolve `config.EmulatorAWS`, `rt.IsHealthy`, `container.ResolveRunningContainerName`, `endpoint.ResolveHost`). *Rejected:* generalizing `lstk aws` into a shared proxy abstraction — override-file generation makes terraform materially different; reuse the discrete helpers, not the whole command body.
- **AWS emulator only, with a precise error.** Discovery (name `localstack-aws`, then AWS image repos on the edge port) can't match Snowflake/Azure. The only gap is the message: when AWS isn't found, check `container.RunningEmulators(...)` and, if a non-AWS emulator is up, fail with an AWS-specific error naming it rather than the generic "not running."
- **New package `internal/iac/terraform/cli` (`tfcli`).** The `internal/iac/` umbrella reserves room for future IaC tooling; no shared `iac` interface yet (YAGNI — the directory seam is enough). Decomposition: `exec.go` (orchestration + subprocess), `schema.go` (`EndpointKeys` + `dedupeAliasKeys` + `ErrInitRequired`), `override.go` (HCL provider-block discovery + render), `defaults.go` (passthrough set `fmt`/`validate`/`version`/`init`), `account.go` (region/account resolution + `DeactivateAccessKey`), `env.go` (scoped env helpers).
- **Encode region/account into the override, not env passthrough.** The AWS provider resolves config block-args → env → shared files. We bake `region`/`access_key` into the generated block (precedence 1) so targeting is deterministic and the flags can't appear to do nothing. *Consequence (deliberate):* this overrides any `region`/`access_key` in the user's own block — the intended "deploy to *this* region/account" forcing.
- **Reduced env surface, `LSTK_TF_`-prefixed.** Support `AWS_ENDPOINT_URL`, `LSTK_TF_CMD`, `LSTK_TF_OVERRIDE_FILE_NAME`, `LSTK_TF_DRY_RUN`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`. lstk-invented vars get the `LSTK_TF_` prefix so a bare `DRY_RUN`/`TF_CMD` already in the user's shell can't collide. Drop `tflocal`'s `SKIP_ALIASES`, `LOCALSTACK_HOSTNAME`, `EDGE_PORT`, `USE_LEGACY_PORTS`, `S3_HOSTNAME`, `USE_EXEC`, `CUSTOMIZE_ACCESS_KEY`, ambient-key derivation, `ADDITIONAL_TF_OVERRIDE_LOCATIONS`, `TF_UNPROXIED_CMDS`, and per-service `<SERVICE>_ENDPOINT`. Env reads stay at the domain boundary (process env, not `config.Get()` — per project rule).
- **Account safety: `DeactivateAccessKey`.** `--account` validated `^\d{12}$`. An `AWS_ACCESS_KEY_ID` env value is not validated but is neutralized: a leading `A` (`AKIA…`/`ASIA…`) is rewritten to `L` before it's encoded, so a real credential in the env is never written to the override or sent to LocalStack. (`secret_key` is always `"test"`.)
- **S3 host prefix.** Virtual-host addressing puts the bucket as a subdomain of the S3 host, so on a virtual-host-capable `*.localstack.cloud` host the `s3` endpoint must carry an `s3.` prefix (`s3.localhost.localstack.cloud`) and `s3_use_path_style=false`; on `127.0.0.1`/`localhost` use path style (`true`) and the bare endpoint. The prefix is gated by the same condition that drives path style; applied only if the host doesn't already start with `s3.`.
- **Leading-only flag parsing.** `--region`/`--account` are recognized only between `terraform`/`tf` and the action; scanning stops at the first non-flag token (robust against `-var` values containing `--region`). Not registered as persistent Cobra flags; `RunE` inspects raw `os.Args` to reject `lstk --account … terraform` (which `DisableFlagParsing` would otherwise silently drop).
- **Subprocess, not `exec`-replace; no spinner.** Run terraform via `exec.CommandContext` (not `syscall.Exec`) so the deferred cleanup runs after it exits. No `StopOnWriteWriter` wrap — terraform's streaming output must stay unobstructed.
- **Provider-block discovery recurses `*.tf`** via `filepath.WalkDir`, skipping hidden dirs (`.terraform` cache, `.git`); a file that fails to parse is logged+skipped, not fatal; no blocks found → one default alias-less provider. Best-effort (won't catch remote modules).

> **Review notes (2026-06-09).** Challenges raised and their dispositions:
> - **"The mutually-exclusive-key filter can be schema-driven."** *Refuted* — the schema has no conflict/deprecated metadata; all endpoint attributes are identical. A hand-maintained table is unavoidable.
> - **"An earlier `validate`-only probe found no endpoint conflict, so there's no problem."** *Refuted* — the warning only fires on `plan` with a resource forcing provider configuration, so the `validate`-only probe was a false negative; the observed `plan -json` output is authoritative.
> - **"Ship a static endpoint list as a pre-`init` fallback."** *Rejected* — drifts across provider versions and masks the real cause; fail with `ErrInitRequired` instead.
> - **"Generalize `lstk aws` into a shared proxy now."** *Rejected* — premature; override generation makes terraform materially different.
> - **"Tag generated files with a marker and silently overwrite our own leftovers."** *Rejected* — a marker isn't reliable authorship; silent clobber risks data loss. Fail loudly.
> - Post-review code refinements folded in: generic `ErrInitRequired` wording (no mention of "provider schema"); restrict to AWS emulator with a named-emulator error; bring up real LocalStack via `lstk start` in e2e (exercises the real user path) rather than driving the Docker SDK.

## Surface

- **New command** — `cmd/terraform.go`: the Cobra command + `tf` alias, registered on root.
- **New domain package** — `internal/iac/terraform/cli` (`tfcli`): binary discovery, schema query, endpoint mapping, override generation, subprocess exec (analogous to `internal/awscli/`). The `internal/iac/` umbrella reserves room for future IaC tooling.
- **Reused, unchanged** — `internal/container` (`ResolveRunningContainerName`), `internal/endpoint` (`ResolveHost`), `internal/runtime` (Docker health), `internal/output` (sink/events), `internal/config` (emulator resolution).
- **New dependency** — `github.com/hashicorp/hcl/v2` to parse user provider blocks. Runtime prereqs: a `terraform`/`tofu` binary on `PATH` and the AWS provider installed via `terraform init`.
- **Docs** — command help documents `--region`/`--account`, the supported/removed env vars, and the `terraform init` prerequisite.

## Risks / Trade-offs

- **HCL parsing of user `.tf`.** Needs `github.com/hashicorp/hcl/v2` (`hclparse`) — idiomatic, dependency accepted; parse failure degrades to a single default provider.
- **Schema-query latency.** `providers schema -json` spawns a process; only invoked for proxied commands (not `fmt`/`validate`/`version`/`init`).
- **Cleanup on hard kill.** `SIGKILL` skips the deferred cleanup → orphaned override; the pre-existing-file check surfaces it next run (same residual risk `tflocal` has under `USE_EXEC`).
- **Endpoint-key drift vs LocalStack-supported services.** Schema may expose keys for services LocalStack doesn't emulate — harmless; Terraform accepts any key, unused entries get no traffic. Avoids the burden of intersecting with a LocalStack service list.

## Migration

Additive — nothing changes for existing lstk users. `tflocal` users switch by running `lstk terraform` / `lstk tf`: lstk resolves the endpoint itself, so the host/port vars (`LOCALSTACK_HOSTNAME`, `EDGE_PORT`, …) are gone and the three lstk-invented knobs are renamed under `LSTK_TF_` (`TF_CMD` → `LSTK_TF_CMD`, `LS_PROVIDERS_FILE` → `LSTK_TF_OVERRIDE_FILE_NAME`, `DRY_RUN` → `LSTK_TF_DRY_RUN`). The full supported/dropped list lives in the command help. No rollback concern beyond not shipping the command.

## Resolved Questions
- **HCL dependency** — accepted (`hashicorp/hcl/v2`).
- **Per-service override casing** — resolved by removal; keys are written verbatim from the schema, so there's no env-to-key mapping to define.

## Open Questions
- **`terraform_remote_state` / S3 backend** — explicitly deferred (Non-Goal v1). Confirm acceptable, or whether a minimal backend endpoint override is needed before first ship. *(Follow-up branch is prototyping S3 backend + remote-state redirection; keep it out of DEVX-800.)*
