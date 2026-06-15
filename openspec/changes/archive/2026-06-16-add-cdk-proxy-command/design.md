## Context

lstk already proxies two AWS-facing tools at LocalStack:
- `lstk aws` ([cmd/aws.go](../../../cmd/aws.go), [internal/awscli/exec.go](../../../internal/awscli/exec.go)) — strips lstk flags, makes a Docker runtime, resolves the AWS emulator container, checks health, resolves `host:port` via `endpoint.ResolveHost`, then execs `aws --endpoint-url …` with mock `AWS_*` env defaults.
- `lstk terraform` ([cmd/terraform.go](../../../cmd/terraform.go), [internal/iac/terraform/cli/](../../../internal/iac/terraform/cli/)) — the same preamble plus `--region`/`--account` handling, then generates a provider-override file before running the real `terraform`.

We want the same ergonomics for AWS CDK. The reference behavior is LocalStack's Node wrapper `cdklocal` (the `aws-cdk-local` package), which wraps the real `aws-cdk` CLI and points its API calls at LocalStack. Critically, `cdklocal`'s mechanism is **version-dependent**:
- For **aws-cdk >= 2.177.0**, `cdklocal` removes ambient AWS configuration from the environment and sets `AWS_ENDPOINT_URL` and `AWS_ENDPOINT_URL_S3` (the latter "must include `.s3.` to correctly identify S3 API calls"), letting CDK's own AWS SDK target the LocalStack **endpoint**. Default endpoint: `http://localhost.localstack.cloud:4566`.
- For **older CDK**, `cdklocal` monkey-patches the in-process Node AWS SDK at runtime (`SdkProvider.withAwsCliCompatibleDefaults`, `_makeSdk`, `ToolkitInfo.lookup`, credential providers, `forcePathStyle`).

A subtlety that matters for this design: even at **>= 2.177.0**, `cdklocal` does **not** rely on env vars alone — the env vars cover the *endpoint*, but S3 *addressing style* (path-style vs. virtual-host) is still applied by in-process SDK patching, which an external subprocess cannot reach. So for `lstk cdk` the env-var mechanism is *endpoint-only*; S3 path style is unreachable. The resulting limitation on the loopback fallback is out of scope for v1 (see the S3 endpoint decision).

lstk runs `cdk` as an external subprocess and cannot reach into CDK's Node SDK, so only the **environment-variable** mechanism is available to us — which means the endpoint is configurable but S3 addressing style is not. This is the central constraint that shapes the design: `lstk cdk` is fundamentally an environment-construction-plus-passthrough command (close to `lstk aws`), **not** a file-generation command like `lstk terraform`. There is no override file, no schema discovery, no HCL parsing, and no cleanup step.

`cdklocal` reference behavior we carry over: invoke the real CDK CLI; default credentials to `test`/`test`; resolve the endpoint from a host/port (we resolve it ourselves instead of from `LOCALSTACK_HOSTNAME`/`EDGE_PORT`); strip conflicting AWS config so a stray profile can't redirect to real AWS; derive the S3 endpoint with an `s3.` host prefix.

## Goals / Non-Goals

**Goals:**
- `lstk cdk <args>` forwards all args to the real `cdk`, streaming stdin/stdout/stderr and propagating the exit code.
- Auto-resolve the LocalStack endpoint from the running AWS emulator — no host/port env vars needed.
- Build a clean, LocalStack-pointed AWS environment for the subprocess (`AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3`, mock creds, region) and strip ambient AWS config that could redirect CDK at real AWS.
- Reuse the terraform command's `--region` parsing, validation, and precedence. CDK does **not** support `--account`: it always operates against the default LocalStack account `000000000000`.
- Gate AWS-contacting subcommands on a running AWS emulator (with the same AWS-specific "wrong emulator" messaging as terraform); run a fixed offline set without that requirement.
- Fail fast and clearly when `cdk` is missing or older than 2.177.0.
- Keep the architecture aligned with lstk conventions: cmd/ wiring only, domain logic in `internal/iac/cdk/cli/`, output via `output.Sink`, diagnostics via `log.Logger`, no direct stdout/stderr prints, no `config.Get()` in domain code.

**Non-Goals:**
- Depending on or shelling out to `cdklocal`/`aws-cdk-local`.
- Reproducing `cdklocal`'s pre-2.177 in-process SDK monkey-patching (impossible for an external subprocess).
- Supporting `cdklocal`-specific knobs like `LAMBDA_MOUNT_CODE`, `BUCKET_MARKER_LOCAL`, `AWS_ENVAR_ALLOWLIST`, `LOCALSTACK_HOSTNAME`, `EDGE_PORT` (lstk resolves the endpoint itself).
- A spinner / progress UI around CDK output.
- Managing `cdk bootstrap` for the user (bootstrap is just another proxied subcommand; lstk does not run it implicitly).
- Pulling real credentials from the ambient AWS credential chain. The account is fixed to the default LocalStack account; `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` are always `test`, and the ambient `AWS_ACCESS_KEY_ID` is never used to select a different account.
- Per-account targeting for CDK. `--account` is intentionally unsupported (see "Why CDK has no --account").

## Decisions

### Command wiring mirrors `lstk aws` / `lstk terraform`
`cmd/cdk.go` defines a Cobra command with `Use: "cdk [args...]"`, `DisableFlagParsing: true`, and the standard `PreRunE` that calls `stripGlobalFlags` + `initConfig(nil)`. It reuses the terraform preamble: `runtime.NewDockerRuntime`, resolve the `config.EmulatorAWS` `ContainerConfig` (via the existing `resolveAWSContainer` helper in `cmd/terraform.go`), `rt.IsHealthy`, `container.ResolveRunningContainerName`, the non-AWS-emulator-running check (`runningNonAWSEmulator`), and `endpoint.ResolveHost`. It strips and validates `--region` at the boundary, rejects `--account` with an actionable error, resolves the effective region, builds `http://host:port`, and hands off to a new domain entry point `cdkcli.Run(...)`.

No CLI alias is added — `cdk` is already short and has no conventional shorthand (unlike terraform's `tf`).

**Reuse, don't duplicate, the flag helpers.** `resolveRegion`, the leading-flag stripper, `rejectPreSubcommandFlags`, and `emitValidationError` already exist for terraform. CDK needs the same `--region` semantics, so these are shared. The shared stripper still *consumes* a leading `--account` (so the tokens cannot leak into the forwarded cdk args), but CDK rejects any non-empty account value rather than acting on it; `resolveAccount`/`accountIDRe`/`DeactivateAccessKey` remain in use by terraform only. The terraform-specific `-chdir` handling is **not** carried over — CDK has no equivalent global directory flag.

**Alternative considered:** generalize `lstk aws`/`terraform`/`cdk` into one shared proxy abstraction. Rejected for now (YAGNI) — the three differ in what they do with the endpoint (CLI flag vs. override file vs. env vars). We share the discrete helpers instead of the whole command body.

### New domain package `internal/iac/cdk/cli/`
Code lives under the existing `internal/iac/` umbrella (which already hosts `internal/iac/terraform/`), in the leaf package `internal/iac/cdk/cli` (Go `package cli`), imported at the single call site as `cdkcli`:
```go
import cdkcli "github.com/localstack/lstk/internal/iac/cdk/cli"
```
All business logic lives here. Suggested decomposition:
- `exec.go` — `Run(ctx, endpointURL, region string, sink output.Sink, logger log.Logger, args []string) error`: locate the binary via `exec.LookPath(cdkCmd())`, verify the version (see below), build the environment, then run `cdk` via `exec.CommandContext` with `os.Stdin`/stdout/stderr wired through; non-zero exit wrapped as `output.NewSilentError` (same as `awscli.Exec`). Wrap in an OpenTelemetry span like `awscli.Exec`.
- `env.go` — `cdkCmd()` reads `LSTK_CDK_CMD` (default `cdk`); `endpointURLOverride()` reads `AWS_ENDPOINT_URL`; `s3EndpointOverride()` reads `AWS_ENDPOINT_URL_S3`. `BuildEnv(base []string, endpointURL, s3Endpoint, region string) []string` produces the subprocess environment (hardcoding `AWS_ACCESS_KEY_ID=test`) (set the LocalStack values, strip ambient AWS config — see "AWS environment construction").
- `version.go` — `CheckVersion(ctx, cdkBin string) error`: run `<cdkBin> --version`, parse the leading `MAJOR.MINOR.PATCH`, and return an actionable error if it is below `2.177.0` or cannot be parsed.
- `defaults.go` — the minimum supported version constant and the fixed offline-subcommand set, with an `IsOffline(args []string) bool` helper that finds the first non-flag token (the subcommand) and looks it up. This mirrors terraform's `IsUnproxied`/`subcommand`.

### AWS environment construction (the core of the change)
`BuildEnv` starts from `os.Environ()` and produces a subprocess environment that deterministically targets LocalStack. Following `cdklocal`'s >= 2.177 behavior, it both **sets** LocalStack values and **removes** ambient AWS configuration that could otherwise win:

Set (overriding any existing value):
- `AWS_ENDPOINT_URL` = resolved `http://host:port` (or `AWS_ENDPOINT_URL` override).
- `AWS_ENDPOINT_URL_S3` = the S3 endpoint (or `AWS_ENDPOINT_URL_S3` override). Derived from the base endpoint with the same `s3.`-prefix logic terraform uses for virtual-host-capable hosts (see "S3 endpoint").
- `AWS_ACCESS_KEY_ID` = `test` (fixed), `AWS_SECRET_ACCESS_KEY` = `test`. A non-12-digit access key id makes LocalStack resolve the default account `000000000000`. Both are set unconditionally, overriding any ambient values (including a 12-digit `AWS_ACCESS_KEY_ID` that would otherwise select a different account).
- `AWS_REGION` = `AWS_DEFAULT_REGION` = resolved region.

Remove (so they cannot redirect CDK at real AWS or override our region):
- `AWS_PROFILE`, `AWS_DEFAULT_PROFILE` — a named profile would pull real credentials/region from `~/.aws`.
- `AWS_SESSION_TOKEN` — a stale real session token alongside our mock key would be rejected or, worse, route to real AWS.

This is the safety-critical part of the design: the whole point of pinning the endpoint **and** clearing profile/session state is that a user with real AWS credentials configured cannot accidentally `lstk cdk deploy` into their real account. It directly mirrors why `cdklocal` strips AWS env vars before invoking `cdk`.

`CDK_DISABLE_LEGACY_EXPORT_WARNING=1` is optionally set to quiet a noisy CDK warning, matching `cdklocal`; it has no correctness impact.

**Alternative considered — pass an `--endpoint`-style flag instead of env vars.** Rejected: the CDK CLI has no global endpoint flag; environment variables are the only supported lever for >= 2.177, which is exactly why the version floor exists.

### Minimum CDK version 2.177.0
Because lstk only sets environment variables (it cannot patch CDK's in-process SDK the way `cdklocal` does for older versions), CDK must be a version that honors `AWS_ENDPOINT_URL`/`AWS_ENDPOINT_URL_S3` — i.e. **>= 2.177.0**. On an older CDK, those variables are ignored and `cdk deploy` would silently target **real AWS** with our mock credentials (best case: an auth failure; worst case, with real creds not fully stripped, a real deploy). That is unacceptable, so `Run` checks `cdk --version` up front and fails with an actionable error naming the minimum version before doing anything else.

**Alternative considered — skip the check and document the requirement.** Rejected: the failure mode (silently hitting real AWS) is severe enough to justify a fast, explicit guard rather than relying on the user reading the help text. The check is one cheap subprocess invocation, only on proxied paths is the running emulator also required, but the version check runs for every invocation since even offline commands behave differently across versions and the floor is a hard product requirement.

### S3 endpoint
`AWS_ENDPOINT_URL_S3` is derived from the base endpoint exactly as terraform derives its `s3` endpoint key value (the `s3Addressing`/`virtualHostCapable` logic in [internal/iac/terraform/cli/override.go](../../../internal/iac/terraform/cli/override.go)):

| Resolved host | `AWS_ENDPOINT_URL_S3` |
|---|---|
| virtual-host-capable `*.localstack.cloud` (e.g. `localhost.localstack.cloud`) | `http://s3.<host>:<port>` |
| `127.0.0.1` / `localhost` | `http://<host>:<port>` (bare) |

This matches `cdklocal`'s documented default (`https://s3.localhost.localstack.cloud:4566`) and the "must include `.s3.`" guidance for the virtual-host case.

**Path-style on the `127.0.0.1` fallback — out of scope for v1.** When `endpoint.ResolveHost` falls back to `127.0.0.1` (DNS-rebind protection blocks `localhost.localstack.cloud`), CDK's S3 asset operations (`bootstrap`, asset-bearing `deploy`s) attempt virtual-host addressing and fail, because the bare CDK CLI exposes `forcePathStyle` only as a code-level client argument with no env/config lever a subprocess can set. Non-S3 services are unaffected. lstk surfaces the DNS fallback as a `SeverityWarning` (`dnsOK == false`); the supported path is ensuring `localhost.localstack.cloud` resolves. A self-sufficient fix is out of scope for v1.

### Offline vs. AWS-contacting subcommands
A fixed set of CDK subcommands never contacts AWS and therefore does not require a running emulator: `init`, `synth` (`synthesize`), `ls` (`list`), `version`, `doctor`, `acknowledge` (`ack`), `context`, `notices`. Everything else (`bootstrap`, `deploy`, `destroy`, `diff`, `import`, `watch`, `rollback`, `gc`, `migrate`, …) is treated as AWS-contacting and is gated on a running AWS emulator, emitting the same actionable error as `lstk terraform` (and the AWS-specific message when a non-AWS emulator is running). The set is fixed and intentionally not configurable, mirroring terraform's `unproxiedCommands`.

The endpoint environment is built and applied for **every** invocation, including offline commands — it is harmless when no API call is made, and it means a `synth` that does perform a context lookup (e.g. `fromLookup`) still routes to LocalStack rather than real AWS. Only the *emulator-running gate* distinguishes offline from AWS-contacting commands. (`synth` can in principle trigger AWS context lookups; classifying it offline means lstk won't require the emulator for it. If a lookup does fire without a running emulator, CDK surfaces its own error — an acceptable, honest failure.)

**Alternative considered — gate every subcommand on a running emulator (simpler).** Rejected: requiring a running LocalStack just to `cdk synth`/`cdk init`/`cdk ls` would be a poor experience for commands that demonstrably never touch AWS, and terraform already set the precedent of a fixed offline set.

### Why CDK has no `--account`
Unlike terraform — where lstk writes the account into a provider override file the tool reads directly — lstk has no in-band way to tell CDK which account to use. The only lever is `AWS_ACCESS_KEY_ID`, and CDK does not treat it as the account: CDK resolves the account by calling STS `GetCallerIdentity` at runtime, and LocalStack then derives the account *back* from the access key id (a 12-digit key → that account; otherwise `000000000000`). That indirect chain (lstk → env → CDK → STS → LocalStack → account, with a CDK-side account cache keyed by access key in the middle) proved unreliable: the account written into `cdk.out` did not consistently track `--account`, defaulting to `000000000000` regardless. Rather than ship a flag that silently doesn't work, CDK pins the default account. `lstk cdk` therefore rejects `--account` with a clear error instead of accepting a value it cannot honor. Multi-account CDK support can be revisited if a reliable, deterministic mechanism (e.g. injecting an explicit `aws://account/region` environment argument) is designed.

**Alternative considered — keep `--account` and inject `aws://<account>/<region>` for the subcommands that accept it (`bootstrap`, `deploy`).** Rejected for now: it requires per-subcommand argument rewriting and only covers commands that take an explicit environment, so it would work inconsistently across the CDK surface. Out of scope for this change; the flag is removed rather than left half-working.

### No spinner; managed subprocess
Per the same reasoning as terraform, no spinner wraps `cdk` and stdout/stderr are **not** routed through `StopOnWriteWriter` (unlike `aws.go`). `cdk deploy`/`bootstrap` stream progress that must remain intact, and CDK prompts interactively (e.g. `--require-approval`), so `os.Stdin` is wired straight through. We run `cdk` as a managed child (`exec.CommandContext`) rather than `syscall.Exec` for consistency with the other proxies and so the OpenTelemetry span closes cleanly; there is no cleanup step to protect (no files are generated), so process replacement would also be safe, but the managed child keeps the code uniform with `awscli`/`tfcli`.

## Risks / Trade-offs

- **Old CDK silently targeting real AWS** → Mitigated by the mandatory `cdk --version` >= 2.177.0 check that aborts before any command runs.
- **Real credentials in the environment** → Mitigated by stripping `AWS_PROFILE`/`AWS_DEFAULT_PROFILE`/`AWS_SESSION_TOKEN` and overriding the key/secret with the fixed mock value `test`. Because CDK never reads `AWS_ACCESS_KEY_ID` from the environment as an account source, no real key can leak through that path.
- **S3 path style on the `127.0.0.1` fallback** → CDK S3 asset operations fail when DNS-rebind protection forces the loopback host (the bare CLI has no env/config lever for path style). Non-S3 services are unaffected. Mitigated by a warning pointing at the DNS fix; a self-sufficient fix is out of scope for v1 (see the S3 endpoint decision).
- **Version-parsing fragility** → `cdk --version` output could change format. Mitigated by parsing only the leading `MAJOR.MINOR.PATCH` and failing safe (treat unparseable output as too old, with a clear error).
- **Subcommand classification drift** → New CDK subcommands won't be in the offline set and will default to requiring the emulator (the safe direction — at worst an unnecessary "start LocalStack" prompt, never an accidental real-AWS call). The set is easy to extend.

## Migration Plan

Additive change — no migration for existing lstk users. Users currently using `cdklocal` can switch to `lstk cdk` once on aws-cdk >= 2.177.0; the supported environment variables and the version requirement are documented in the command help, `README.md`, and `CLAUDE.md`. No rollback concerns beyond not shipping the command.

## Open Questions

None — resolved during design.

**Bootstrap UX (resolved).** `lstk cdk` does not detect a not-yet-bootstrapped environment or hint `lstk cdk bootstrap`; the not-bootstrapped error is left to CDK to surface. This keeps lstk a transparent passthrough and is consistent with the non-goal of not managing `cdk bootstrap` implicitly.
