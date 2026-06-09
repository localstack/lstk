# `lstk terraform` — Implementation Plan

> **For implementers:** work this plan task-by-task. Each behavior gets a failing test FIRST, observed RED, before implementation. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `lstk terraform` / `lstk tf` proxies all args to the real `terraform` (or `tofu`) binary against a running LocalStack AWS emulator — auto-resolving the endpoint, generating an AWS provider override with endpoint keys discovered dynamically from the provider schema, then cleaning it up.

**Architecture:** `cmd/terraform.go` (Cobra, mirrors `cmd/aws.go`) → domain package `internal/iac/terraform/cli` (`tfcli`). All logic in the package; cmd does wiring + flag-boundary parsing only. Full reasoning, decisions, and the **four critical pre-design findings** (mutually-exclusive keys; apply-not-plan; no-fallback-pre-init; override-file authorship): [`docs/specs/2026-06-09-devx-800-terraform-cli-design.md`](../specs/2026-06-09-devx-800-terraform-cli-design.md) — **read it first.**

**Tech Stack:** Go; `github.com/hashicorp/hcl/v2` (`hclparse`); `exec.CommandContext`; `creack/pty` (interactive tests N/A here — terraform streams, no PTY needed); reuses `internal/container`, `internal/endpoint`, `internal/runtime`, `internal/output`, `internal/config`.

**Branch:** `devx-800-terraform-cli` off `main`.

---

## Context the executor needs

- **Mirror the `lstk aws` preamble.** `grep -n . cmd/aws.go` — copy the runtime/health/discovery/host-resolution block; `internal/awscli/exec.go` is the analog for `tfcli.Run` (silent-error wrapping, OTel span).
- **Schema JSON path:** `provider_schemas["registry.terraform.io/hashicorp/aws"].provider.block.block_types["endpoints"].block.attributes` → keys = endpoint names, written **verbatim** (no case transform). Match the AWS provider across registry hosts so OpenTofu's registry works too.
- **`endpointAliasGroups` table** (the hand-maintained dedup, Finding 1): captured from `terraform plan -json` on AWS provider 6.48 (39 groups incl. `databrew`/`gluedatabrew`, `lexmodels`/`lex…`) ∪ `simpledb`/`sdb`. `dedupeAliasKeys` keeps the first present member per group.
- **`tfcli.Run` signature:** `Run(ctx, endpointURL, region, account string, sink output.Sink, logger log.Logger, args []string) error`.
- **Project rules:** no `config.Get()` in `tfcli` (env read at the boundary); no direct stdout/stderr prints (emit via `output.Sink`); errors checked. **`make test` runs in pre-commit; NEVER `--no-verify`.**

---

## Task 1: Tests, proven RED (the behavioral contract)

Write these first and watch them fail. They ARE the behavioral spec; there is no separate SHALL document.

**Unit (`internal/iac/terraform/cli/*_test.go`):**
- [ ] 1.1 `schema_test.go` — parse a captured `providers schema -json` fixture (AWS provider 4.0+) → expected keys; missing-provider / non-zero-exit → `ErrInitRequired`; present-but-empty → plain error.
- [ ] 1.2 `schema_test.go` — `dedupeAliasKeys`: a set containing multiple members of an alias group collapses to the first present member; non-grouped keys pass through. (Finding 1.)
- [ ] 1.3 `override_test.go` — render: default (alias-less) provider; multiple aliases (one block each, alias preserved); custom file name; path-style on (`127.0.0.1`, bare `s3`) vs off (`*.localstack.cloud`, `s3.`-prefixed); `s3` not double-prefixed.
- [ ] 1.4 `account_test.go` — `DeactivateAccessKey`: `AKIA…`/`ASIA…` → leading `L`; `test`/12-digit/empty untouched. Region/account precedence (`--flag` → env → default).
- [ ] 1.5 `cmd/terraform_test.go` — leading-only flag parser: `--flag value` and `--flag=value`; missing value errors; scan stops at first non-flag token (flag after the action is forwarded verbatim); invalid `--account` rejected.

**Integration (`test/integration/terraform_cmd_test.go`, stub `terraform` + alpine emulator stand-in):**
- [ ] 1.6 forwards args + propagates exit code; 1.7 "not running" error + terraform NOT invoked; 1.8 `fmt`/`validate`/`version`/`init` skip override + need no emulator (even with `--region`/`--account` present → stripped, ignored); 1.9 `LSTK_TF_DRY_RUN` writes override + skips terraform, region/account encoded; 1.10 pre-existing override → clear failure, file NOT deleted (Finding 4); 1.11 leading flags stripped + encoded, invalid `--account` fails pre-invoke; 1.12 positional rules (flag after action forwarded; before subcommand rejected); 1.13 schema unavailable → "run `terraform init` first", terraform NOT invoked, no override (Finding 3); 1.14 `LSTK_TF_CMD=tofu` invokes a `tofu` stub; 1.15 non-AWS emulator running → AWS-specific error naming it.

Isolated `$HOME` via `testEnvWithHome` everywhere. Commit RED tests + impl together if the hook blocks a red-only commit.

## Task 2: Domain scaffolding
- [ ] 2.1 Create `internal/iac/terraform/cli/` (`package cli`); `Run(...)` entry point (signature above).
- [ ] 2.2 `defaults.go` — passthrough set `fmt`/`validate`/`version`/`init` (`init` bootstraps the provider, runs directly w/o override). No fallback endpoint list.
- [ ] 2.3 `env.go` — scoped helpers: `tfCmd()` (`LSTK_TF_CMD`, default `terraform`), `overrideFileName()` (`LSTK_TF_OVERRIDE_FILE_NAME`, default `localstack_providers_override.tf`), `dryRun()` (`LSTK_TF_DRY_RUN`), `endpointURLOverride()` (`AWS_ENDPOINT_URL`). Do NOT read `AWS_DEFAULT_REGION`; do NOT support `SKIP_ALIASES`.

## Task 3: Schema discovery (→ 1.1, 1.2 GREEN)
- [ ] 3.1 `schema.go` `EndpointKeys(ctx, tfBin, workdir)` — run `<tfBin> providers schema -json`, navigate the path, return keys verbatim; match the AWS provider across registry hosts (OpenTofu).
- [ ] 3.2 Error cases: failed query / provider absent → `ErrInitRequired` (generic, end-user wording — no "provider schema" mention); present-but-empty → plain error.
- [ ] 3.3 `dedupeAliasKeys` + `endpointAliasGroups` table (Finding 1).

## Task 4: Override generation (→ 1.3, 1.10 GREEN)
- [ ] 4.1 `override.go` — recurse `*.tf` via `filepath.WalkDir` using `hcl/v2`, discover `aws` provider blocks + `alias`/`region`; skip hidden dirs (`.terraform`/`.git`); log+skip unparseable files; fall back to one default provider if none found.
- [ ] 4.2 Render one block per alias: resolved `access_key`/`region`, `secret_key="test"`, skip flags, preserved `alias`, `endpoints {}` (deduped keys, all → resolved endpoint).
- [ ] 4.3 `s3_use_path_style` from host; `s3.` host prefix when path-style off (unless already prefixed). (Design § S3 host prefix.)
- [ ] 4.4 Pre-existing-file safety check → clear error, no delete (Finding 4); return written paths for cleanup.

## Task 5: Region/account boundary (→ 1.4, 1.5 GREEN)
- [ ] 5.1 `account.go` — region (`--region`→`AWS_REGION`→`us-east-1`) + account (`--account`→`AWS_ACCESS_KEY_ID`→`test`); validate `--account` `^\d{12}$`; `DeactivateAccessKey` on env value.
- [ ] 5.2 `cmd/terraform.go` boundary parser — leading-only, both flag forms, stop at first non-flag, missing-value error; reject flags before the subcommand via raw `os.Args`.

## Task 6: Execution orchestration (→ 1.6–1.14 GREEN)
- [ ] 6.1 `exec.go` — `exec.LookPath(tfCmd())` (clear "install terraform" error); detect unproxied subcommand → run direct; proxied → schema discover → generate override → `defer` cleanup (removes only lstk-written paths; runs on success/error/cancel).
- [ ] 6.2 `LSTK_TF_DRY_RUN` → write override, skip terraform. Run via `exec.CommandContext` with `os.Stdin` + passed writers; non-zero → `output.NewSilentError`; NO spinner wrap. OTel span (mirror `awscli/exec.go`).

## Task 7: Command wiring
- [ ] 7.1 `cmd/terraform.go` `newTerraformCmd(cfg *env.Env)`: `Use: "terraform [args...]"`, `Aliases: ["tf"]`, `DisableFlagParsing: true`, `PreRunE: initConfig(nil)`; `Long` help documents `--region`/`--account`, supported/removed env vars, "LocalStack must be running", "run `terraform init` first".
- [ ] 7.2 `RunE`: `aws.go` preamble + region/account parse/resolve; build `http://host:port`; `PlainSink` + logger; call `tfcli.Run(...)`. Register alongside `newAWSCmd` in root.

## Task 8: e2e matrix (real terraform + AWS provider + LocalStack)
Gated on Docker + a real `terraform`/`tofu` + auth token; CI installs both on Linux shards (`hashicorp/setup-terraform` / `opentofu/setup-opentofu`, wrappers off); skip elsewhere. **`apply`, not bare `plan`** (Finding 2). Samples live as readable files under `test/integration/test-samples/iac/terraform/<name>/`, copied to a temp dir per test.
- [ ] 8.1 Harness in `test/integration/localstack_test.go` — bring up real LocalStack via `runLstk(..., "start")` (exercises the real user path); copy sample; assert override generated during run + removed after.
- [ ] 8.2 single-dir project — `init`+`apply` succeed, override cleaned up.
- [ ] 8.3 sub-dir aliased provider represented (dry-run capture); `.terraform` ignored.
- [ ] 8.4 single no-alias provider — one block, `apply` succeeds. 8.5 multiple aliases + default — one block each, `apply` through both succeeds.
- [ ] 8.6 explicit FIPS endpoint in user block — override wins (dry-run asserts FIPS gone; `apply` succeeds where public FIPS+mock-creds would fail).
- [ ] 8.7 provider versions `~> 4.0` and latest — `apply` succeeds for both. 8.8 `tofu` top-level flow.

## Task 9: Verification
- [ ] 9.1 `make build && make test` GREEN; `make lint`.
- [ ] 9.2 `make test-integration RUN=Terraform` (Docker + token + terraform/tofu).
- [ ] 9.3 Manual: `lstk start`; in a sample dir `lstk tf init && lstk tf apply -auto-approve`; confirm bucket created in LocalStack, override file gone after.
- [ ] 9.4 Update command help / README env-var table. Confirm the Open Question (S3 backend / `terraform_remote_state`) stays out of scope for DEVX-800.
