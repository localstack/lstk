## Context

lstk already ships two first-party IaC proxy commands — `lstk terraform` and `lstk cdk` — that let users run the real tool against LocalStack without the third-party `tflocal`/`cdklocal` wrappers. The AWS SAM ecosystem has an equivalent third-party wrapper, `samlocal` (the `aws-sam-cli-local` pip package), which configures the SAM CLI to talk to LocalStack. This change adds a first-party `lstk sam` proxy so SAM users get the same experience.

The two existing proxies sit at opposite ends of a spectrum:
- **terraform** generates a provider-override `.tf` file (because the AWS Terraform provider needs per-service endpoint blocks discovered from the provider schema).
- **cdk** is purely environment-variable driven: it sets `AWS_ENDPOINT_URL`/`AWS_ENDPOINT_URL_S3`, mock credentials, strips ambient AWS config, version-gates the binary, and execs the tool with stdio wired through.

The SAM CLI runs its cloud operations (`deploy`, `package`, `sync`, `delete`, `logs`, `traces`, parts of `list`) through boto3/botocore, which honors `AWS_ENDPOINT_URL`. So SAM fits the **environment-variable model like CDK**, not the file-generation model of Terraform.

Hands-on testing (see the comparison below) showed SAM is *not* a straight CDK clone, however. It lands as a third point in the design space: **CDK's env-var mechanism + Terraform's account model + the simplest endpoint of the three**. Specifically, testing confirmed:
- Only `AWS_ENDPOINT_URL` is needed — `http://localhost:4566` works with no S3-specific endpoint and no path-style fix, because SAM's botocore auto-selects path-style addressing against a `localhost`/IP host. (CDK needs an `s3.`-prefixed `AWS_ENDPOINT_URL_S3` because the JS SDK defaults to virtual-host addressing.)
- SAM honors `AWS_DEFAULT_REGION`, **not** `AWS_REGION` (it resolves `--region` itself and passes it explicitly into the boto3 session, with its env default reading `AWS_DEFAULT_REGION`).
- SAM passes `AWS_ACCESS_KEY_ID` straight through and LocalStack uses it as the account id — confirmed with a custom 12-digit key — so `--account` should be supported, exactly like Terraform (and unlike CDK, which rejects it).
- The minimum SAM CLI version that honors `AWS_ENDPOINT_URL` is `1.95.0`.

`lstk sam` will reuse the shared `cmd/iac.go` command-boundary helpers and `internal/endpoint.ResolveHost`. It does **not** need `internal/endpoint.S3Addressing`.

| Dimension | terraform | cdk | sam (tested) |
|---|---|---|---|
| Mechanism | override `.tf` file | env vars | env vars |
| Endpoint | per-service in file | `AWS_ENDPOINT_URL` + `AWS_ENDPOINT_URL_S3` (s3. prefix) | just `AWS_ENDPOINT_URL` |
| S3 path-style | `s3_use_path_style` | s3.-host + DNS warning | not needed (botocore auto path-style) |
| Region env | per-provider in file | `AWS_REGION` + `AWS_DEFAULT_REGION` | `AWS_DEFAULT_REGION` (load-bearing) |
| Account | `--account` ✅ (key = account) | `--account` ❌ rejected | `--account` ✅ (key = account) |
| Credentials | `access_key`=account | `access_key`=`test` fixed | `access_key`=account |
| Min version | — | 2.177.0 | 1.95.0 |

Relevant existing code:
- `cmd/cdk.go` — Cobra wiring to mirror.
- `internal/iac/cdk/cli/{exec,env,version,defaults}.go` — domain logic to mirror under `internal/iac/sam/cli/`.
- `cmd/iac.go` — shared helpers (`stripLeadingIaCFlags`, `resolveRegion`, `resolveAccount`, `requireRunningAWSEmulator`, `rejectPreSubcommandFlags`, `emitValidationError`, `resolveAWSContainer`) — reused as-is.
- `internal/endpoint/endpoint.go` — `ResolveHost` — reused as-is. `S3Addressing` is not needed (see Decision 2).

## Goals / Non-Goals

**Goals:**
- Provide `lstk sam` that transparently forwards arguments to the real `sam` binary with the subprocess environment configured for LocalStack.
- Reuse the existing IaC command-boundary helpers and endpoint resolution so behavior (region parsing, emulator gating, error rendering) is consistent with `lstk cdk`.
- Be functionally equivalent to `samlocal` for the common case: ZIP-based Lambda functions and standard CloudFormation/S3/IAM/STS operations (`deploy`, `sync`, `package`, `delete`, `logs`, …).
- Support `--account` (like Terraform), since SAM passes the access-key id through as the account.
- Gate AWS-contacting subcommands on a running AWS emulator; let offline/local subcommands run without one.
- Version-gate the SAM CLI (minimum `1.95.0`) so lstk never silently targets real AWS via an old binary that ignores `AWS_ENDPOINT_URL`.
- No new Go module dependencies.

**Non-Goals:**
- Reimplementing or vendoring `samlocal`/`aws-sam-cli-local`.
- **Full parity with `samlocal`'s in-process monkeypatches** — see "Parity with samlocal" below. Specifically out of scope for v1: image/container-based Lambda (ECR) deploys and nested CloudFormation stack template export. Both require patching SAM's Python internals, which a subprocess wrapper structurally cannot do (Open Questions track whether/how to address them).
- Solving in-container networking for `sam local invoke`/`start-api`/`start-lambda` so a Lambda's *runtime* AWS calls reach LocalStack from inside the SAM-spawned Docker container. That requires a container-reachable endpoint (e.g. `host.docker.internal`) and is out of scope for the first version (see Risks / Open Questions).
- Terraform-style file generation; SAM needs none.
- Setting `AWS_ENDPOINT_URL_S3` or any S3 path-style configuration; SAM does not need it (it is only honored as a user override).
- Reading `AWS_DEFAULT_REGION` to *decide* the region — lstk's region resolution stays consistent with tf/cdk (`--region` → `AWS_REGION` → `us-east-1`); SAM's `AWS_DEFAULT_REGION` dependency only affects what lstk *writes* to the subprocess.

## Decisions

### Decision 1: Mirror the CDK proxy (env-based), not the Terraform proxy (file-based)
SAM's cloud operations use botocore, which reads `AWS_ENDPOINT_URL`/`AWS_ENDPOINT_URL_S3`. So the entire LocalStack redirection is achievable through the subprocess environment with no generated files.

- **Approach**: Create `internal/iac/sam/cli/` as a structural copy of `internal/iac/cdk/cli/` — `exec.go` (locate binary, version-check, build env, exec with stdio wired, wrap non-zero exit as silent error), `env.go` (`samCmd()`/`LSTK_SAM_CMD`, endpoint overrides), `version.go`, `defaults.go` (offline set + subcommand scanner). `cmd/sam.go` mirrors `cmd/cdk.go`.
- **Alternatives considered**: Generalizing the CDK package into a shared "env proxy" abstraction parameterized by binary name/min version/offline set. Rejected for now — the two would share ~90% code, but the existing codebase keeps terraform and cdk as separate sibling packages rather than abstracting them, and premature generalization would obscure SAM-specific divergences (offline set, `sam local`, telemetry env). Following the established sibling-package pattern keeps the change consistent and reviewable. A future refactor could extract the shared core once a third env-based proxy exists.

### Parity with samlocal

`samlocal` (the `aws-sam-cli-local` Python script) and `lstk sam` use fundamentally different mechanisms, which sets a hard ceiling on parity:

```
   samlocal (Python)                      lstk sam (Go)
   ─────────────────                      ─────────────
   imports samcli, monkeypatches it       sets env vars, then exec()s sam
   in-process, then calls main.cli()      as a separate black-box subprocess
   → can rewrite ANY SAM internal         → can only influence via env + flags
```

What `samlocal` does, and how `lstk sam` compares:

| samlocal action | lstk sam | Parity |
|---|---|---|
| Patch `boto3.Session.client` to inject `endpoint_url` | `AWS_ENDPOINT_URL` | ✅ equivalent for SAM ≥ 1.95.0 |
| Set `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` = `test` (if unset) | sets them (account-derived; `--account` supported) | ✅ richer |
| Honor `AWS_ENDPOINT_URL` / `EDGE_PORT` / `LOCALSTACK_HOSTNAME` | `AWS_ENDPOINT_URL` (lstk-resolved); the deprecated `EDGE_PORT`/`LOCALSTACK_HOSTNAME` are not honored | ✅ (deprecated knobs dropped) |
| Patch `is_ecr_url` + `prompt_image_repository` + `ECRUploader.upload` to make **image-based Lambda (ECR)** deploys work | — | ❌ **gap** (Open Question) |
| Patch `do_export` to bypass **nested CFN stack** template export ([localstack#4965](https://github.com/localstack/localstack/issues/4965)) | — | ❌ **gap** (Open Question) |

Notable places where `lstk sam` is *more* robust than `samlocal`: it strips `AWS_PROFILE`/`AWS_DEFAULT_PROFILE`/`AWS_SESSION_TOKEN` (samlocal leaves them, relying on its endpoint patch to override), and it sets the region deterministically (samlocal does not set a region at all). The cost of the cleaner env-only approach is the SAM ≥ 1.95.0 floor (Decision 3) — samlocal needs no version floor because its monkeypatches are version-independent.

**Net:** functionally equivalent to `samlocal` for ZIP-based Lambdas + standard CloudFormation/S3/IAM/STS operations (the common case). The two ECR/nested-stack gaps are SAM-internal behaviors no env var or flag can reach on stock SAM, so they are deferred to the Open Questions rather than scoped into v1.

### Decision 2: Environment variables set for the `sam` subprocess
A pared-down version of CDK's `BuildEnv`. Set (overriding ambient):
- `AWS_ENDPOINT_URL` = resolved LocalStack endpoint (or the user's `AWS_ENDPOINT_URL` override). No S3-specific endpoint is derived or set — testing confirmed plain `AWS_ENDPOINT_URL` covers `sam deploy --resolve-s3` and managed-bucket upload, because botocore auto-uses path-style addressing for a `localhost`/IP host.
- `AWS_ACCESS_KEY_ID` = resolved account (see Decision 6), `AWS_SECRET_ACCESS_KEY=test`.
- `AWS_REGION` **and** `AWS_DEFAULT_REGION` = resolved region. Both are set even though SAM only reads `AWS_DEFAULT_REGION`: it's free, matches CDK, overwrites any stale ambient `AWS_REGION`, and keeps the load-bearing `AWS_DEFAULT_REGION` correct.
- **Not** `SAM_CLI_TELEMETRY`. lstk deliberately leaves the user's SAM telemetry preference untouched. It was initially considered (analogous to CDK's `CDK_DISABLE_LEGACY_EXPORT_WARNING`) but the two are different in kind: the CDK var suppresses a cosmetic console warning, whereas `SAM_CLI_TELEMETRY=0` silently opts the user out of data collection — a privacy choice that belongs to the user (via SAM's first-run prompt, their `~/.aws-sam` config, or their own `SAM_CLI_TELEMETRY` export), not to a transparent proxy. It is also orthogonal to the package's stated rule of setting/stripping only what could *misroute a deploy* (telemetry cannot), and samlocal does not touch it either. The Azure-CLI precedent (lstk disables telemetry under `AZURE_CONFIG_DIR`) does not transfer: that is an lstk-owned throwaway config dir, whereas `lstk sam` runs in the user's own environment over their real `sam` binary. A user who has set `SAM_CLI_TELEMETRY` keeps it — the value passes through untouched.

Strip from the subprocess env (mirror CDK `strippedKeys`): `AWS_PROFILE`, `AWS_DEFAULT_PROFILE`, `AWS_SESSION_TOKEN`. `AWS_ENDPOINT_URL_S3` is deliberately **not** stripped and **not** set — if a user sets it, it flows through as an escape hatch for an exotic S3 addressing case. Empty-valued managed entries are skipped so they never clobber inherited values with `""`.

- **Alternatives considered**: Mirroring CDK exactly (set `AWS_ENDPOINT_URL_S3` with an `s3.` host + path-style). Rejected — testing showed SAM needs neither, and the extra machinery (`S3Addressing`, the DNS-fallback warning) would be dead weight. Passing `--region`/endpoints as SAM CLI flags was also rejected — SAM has no global `--endpoint-url`, so the env approach is uniform and is what `samlocal` relies on.

### Decision 3: Minimum SAM CLI version gate
Older SAM CLIs bundle a botocore that ignores `AWS_ENDPOINT_URL`, which would silently target real AWS. Mirror CDK: run `sam --version`, parse `MAJOR.MINOR.PATCH`, and refuse to proceed below the minimum with an actionable upgrade message.

- **Decision**: Minimum **SAM CLI `1.95.0`** — the version from which `AWS_ENDPOINT_URL` is honored, established by testing. Recorded in `version.go` and the help text.
- **Alternatives considered**: No version gate (rely on the user). Rejected — silently hitting real AWS is the exact failure mode the CDK gate exists to prevent.

### Decision 4: Offline vs AWS-contacting subcommand classification
Reuse the CDK `IsOffline`/`subcommand` scanner pattern (skip flags, skip value-taking global flags, return first bare token), with a SAM-specific offline set.

**The gate is UX-only, not a safety mechanism.** Unlike CDK (where the gate/version check exist to prevent a silent *real-AWS* call), lstk always sets `AWS_ENDPOINT_URL` to LocalStack for `sam`, so *every* command targets LocalStack regardless of classification. Misclassification therefore can never cause a wrong-target call — the only consequence is whether the user sees lstk's friendly "LocalStack is not running" message or SAM's raw botocore "Could not connect to the endpoint URL" error. This means we classify by **top-level token only** and pick the common-case behavior for the few mixed commands rather than chasing second-level precision.

Classification was established empirically against SAM CLI 1.151.0 (ambiguous commands tested against a dead endpoint):

- **Offline (no running emulator required, env still applied):** `docs`, `init`, `build`, `validate` (validates locally — modern SAM no longer calls CloudFormation `ValidateTemplate`, confirmed including `--lint`), `local` (all children: `invoke`, `start-api`, `start-lambda`, `generate-event`, `callback`, `execution`), `pipeline` (favors the common `pipeline init`), plus the no-subcommand forms `--version`, `--info`, `-h`/`--help`.
- **AWS-contacting (require running AWS emulator):** `deploy`, `sync`, `package` (uploads to S3), `delete`, `logs`, `traces`, `list` (favors the stack-querying `endpoints`/`stack-outputs`/`list resources --stack-name`), `remote` (all children), `publish`.
- **`sam local invoke`/`start-api`/`start-lambda`:** offline for the gate; the LocalStack env is still applied. Known limitation: the function's own runtime calls won't reach LocalStack without container-network endpoint configuration (Non-Goal / Risk).

Two commands are mixed at the top-level-token granularity; both are classified by their common case, with the niche case degrading only to a raw connection error (never a wrong target):
- `list` → AWS-contacting. `list resources` *without* `--stack-name` is actually offline, so it is needlessly gated (fixed by starting the emulator).
- `pipeline` → offline. `pipeline bootstrap` actually contacts AWS, so against a stopped emulator it yields a connection error instead of lstk's message.

- **Decision**: Encode the two sets above in `defaults.go` as the top-level offline set `{docs, init, build, validate, local, pipeline}` (everything else AWS-contacting). No second-level classification.

### Decision 5: Command naming — `lstk sam`
Per the project convention (`lstk terraform`, `lstk cdk` name the underlying tool, not the `tflocal`/`cdklocal` wrapper), the command is `lstk sam`, not `lstk samlocal`. No alias is added initially (CDK has none; Terraform's `tf` alias is the lone exception). The help text references SAM and that no `samlocal` install is needed.

### Decision 6: Region/account handling — reuse helpers, support `--account` (Terraform model)
Reuse `stripLeadingIaCFlags(passthrough, false)` (no `-chdir` for SAM), `resolveRegion`, `resolveAccount`, `rejectPreSubcommandFlags`, `emitValidationError`.

- **Region**: `resolveRegion` is used unchanged — reading precedence `--region` → `AWS_REGION` → `us-east-1`, consistent with tf/cdk and deliberately not consulting `AWS_DEFAULT_REGION` for the *decision*. The resolved value is then written to both `AWS_REGION` and `AWS_DEFAULT_REGION` for the subprocess (Decision 2).
- **Account**: Support `--account` via `resolveAccount` (12-digit validation, `DeactivateAccessKey` for a stray real `AKIA…`/`ASIA…` key, fallback to `test`). Testing confirmed SAM passes `AWS_ACCESS_KEY_ID` through and LocalStack maps it to the account, including a custom 12-digit id — so unlike CDK (which rejects `--account` due to an inconsistent STS round-trip), SAM follows the Terraform model. The resolved account is written to `AWS_ACCESS_KEY_ID`.
- **Alternatives considered**: Rejecting `--account` like CDK. Rejected — testing showed a custom account id works end-to-end, so there's no reason to deny it.

## Risks / Trade-offs

- **`sam local` runtime calls don't reach LocalStack** → The env we set affects the `sam` process (and its build/deploy boto3 calls), but a function executed by `sam local invoke` runs in a separate Docker container where `AWS_ENDPOINT_URL=http://127.0.0.1:4566`/`localhost.localstack.cloud` may not resolve to the host's LocalStack. Mitigation: document as a known limitation for v1; treat `local` as offline for gating; a follow-up can inject a container-reachable endpoint (e.g. `http://host.docker.internal:4566`) into the function environment. This matches `samlocal`'s own historical limitations.
- **S3 addressing edge cases beyond what was tested** → Testing covered a `sam deploy` round-trip on a plain `localhost` endpoint (botocore auto path-style). An exotic case (e.g. an unusual `--s3-bucket` name or a virtual-host-only setup) could in theory still need explicit S3 configuration. Mitigation: lstk leaves `AWS_ENDPOINT_URL_S3` as a pass-through escape hatch — a user can set it and botocore honors it — without lstk having to derive or warn.
- **Offline-set misclassification** → If an AWS-contacting command is wrongly marked offline, it could run without the emulator and fail confusingly; if an offline command is wrongly gated, it needlessly requires a running emulator. Mitigation: the safe default is to require the emulator when unsure; finalize the set from `sam --help` (Decision 4).
- **Coupling to SAM CLI behavior** → SAM could change how it consumes endpoint env vars across versions. Mitigation: the version gate documents the supported floor; integration tests use a fake `sam` to lock the contract (args forwarded, env injected) independent of the real CLI.
- **Silent parity gaps vs samlocal** → A user migrating from `samlocal` to `lstk sam` for an **image-based Lambda** or **nested-stack** project will hit a failure (likely an ECR-URL validation/push error, or a nested-template export error), with no obvious signal that the cause is `lstk sam`'s missing monkeypatches rather than their setup. Mitigation: state the limitation explicitly in `lstk sam --help` and `CLAUDE.md`, and point affected users to `samlocal` as the fallback until the Open Questions are resolved.

## Migration Plan

Additive change — a new command with no effect on existing behavior. No data migration, no config changes, no breaking changes. Rollback is removing the command registration; the deprecated third-party `samlocal` continues to work for users who prefer it. Ship behind no flag; document in `CLAUDE.md` and command help.

## Open Questions

- **Image/container-based Lambda (ECR) support** — `samlocal` makes this work via three monkeypatches (broaden `is_ecr_url`, rewrite the ECR repo host in `prompt_image_repository`/`ECRUploader.upload`). A subprocess wrapper can't patch SAM internals. A source spike against stock SAM 1.151.0 found the gap has *partly* closed upstream but is not gone:
  - `is_ecr_url` now natively accepts a bare `localhost[:port]/repo` and `127.0.0.1[:port]/repo`, but **rejects** `localhost.localstack.cloud:4566/repo`, any `http://`-prefixed form, and the virtual-host `<id>.dkr.ecr.<region>.localhost.localstack.cloud:4566/repo` form (verified empirically).
  - `ECRUploader.upload`'s rewrite is a no-op for an explicitly-provided localhost URL (it only rewrites repos containing `amazonaws.com`); `prompt_image_repository` only fires in guided (`--guided`) mode.
  - **Net:** image-based Lambda is reachable on stock SAM *only* if the user passes `--image-repository` with a bare `127.0.0.1`/`localhost` host (no scheme, not `localhost.localstack.cloud`, not the virtual-host ECR URL). This conflicts with lstk's `endpoint.ResolveHost`, which prefers `localhost.localstack.cloud` — so the obvious image-repo value is the rejected one.
  - **Sub-question raised:** should `lstk sam` force the loopback host (`127.0.0.1`/`localhost`) for `AWS_ENDPOINT_URL` instead of `localhost.localstack.cloud`? It would make image repos validate cleanly and costs nothing for non-ECR ops (SAM uses path-style S3 on any host), at the price of diverging from how `lstk cdk` resolves the host. Undecided.
  - **Remaining avenues:** (a) document the narrow `127.0.0.1`-host recipe and defer broader support to `samlocal`; (b) force the loopback host (sub-question above); (c) a larger future change (upstream a SAM fix, or ship a thin Python shim). Still needs an end-to-end image deploy to confirm the `127.0.0.1`-host path actually completes (login + docker push + Lambda pull).
- **Nested CloudFormation stack export** — `samlocal` patches `do_export` to bypass re-exporting a nested template whose path points at LocalStack S3 ([localstack#4965](https://github.com/localstack/localstack/issues/4965)). A source spike found stock SAM's `is_s3_url` still requires `amazonaws.com`/`s3://` and does **not** recognize LocalStack S3 URLs — but `do_export` only re-examines a path when it already contains a localhost S3 URL (the multi-pass #4965 scenario); a normal single-pass `sam deploy` from local nested-template *files* uploads them to LocalStack S3 without the patch. Open question: does the #4965 edge still bite on current LocalStack + SAM, or is single-pass deploy sufficient for v1? Needs an end-to-end nested-stack deploy to confirm before deciding document-as-limitation vs. pursue a fix.
- **`sam local` networking** — out of scope for v1, but decide whether to document a manual `AWS_ENDPOINT_URL` override recipe for users who need it now.

_Resolved by testing/source review:_ `SAM_CLI_TELEMETRY` is deliberately not set (Decision 2); minimum SAM version is `1.95.0`; only `AWS_ENDPOINT_URL` is needed (no S3 variant / path-style); `--account` works (Terraform model); SAM honors `AWS_DEFAULT_REGION`, not `AWS_REGION`; the offline subcommand set is finalized (Decision 4); `lstk sam` is functionally equivalent to `samlocal` for the common case, with two known gaps (above) inherent to the subprocess approach.
