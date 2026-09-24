## Why

An `lstk aws` invocation fails for two unrelated reasons: lstk could not reach the emulator, or the AWS CLI rejected what the user typed. The `lstk_command` event records both identically, so the analytics error ranking counts users' own CLI mistakes as lstk failures — that is how "exit status 252" entered the top of the error list (DEVX-1003).

Marking the failure's origin fixes the ranking. Fixing the *rate* additionally needs the population an invocation belongs to: the [Daily command failure rate](https://grafana.data.aws.localstack.cloud/d/cli-lstk-product-analytics?viewPanel=19) panel divides non-zero exits by completed commands, and a successful `lstk aws s3 ls` is indistinguishable there from a successful `lstk start`. Until it is, "how often do lstk's own commands fail?" has no answer.

A third distortion is interruptions. A wrapped tool that traps Ctrl-C exits 1 or 130, which the pipe today reads as a failure; only the `context canceled` string on lstk's own commands is recognised as a cancellation.

## What Changes

The event gains three **raw observations**; every interpretation (whose failure was it?) moves into the `fct_lstk_command` pipe, where the rule for historical rows must live anyway.

- **Add `parameters.proxied`** (bool, always emitted): the invocation asked lstk to run a wrapped third-party tool, regardless of whether that tool started or succeeded. True for `aws`, `az`, `cdk`, `sam`, `terraform`; false for lstk's own commands, including `az start-interception`/`stop-interception` and extensions, which are lstk's own code shipped separately.
- **Add `result.proxy_exit_code`** (int, present only when the wrapped tool ran to completion): the tool's own exit code, `0` included. Absent on lstk's own commands and on proxy invocations that failed before the tool ran (preflight, Docker down, tool not on PATH). Presence is the "tool ran" signal.
- **Add `result.cancelled`** (bool, always emitted): lstk's own signal context was cancelled (SIGINT/SIGTERM, or the TUI was quit) by the time the invocation finished. Determined from lstk's context, never from the child's exit code — a tool that traps the signal exits with an ordinary code, and Windows has no signal exit code at all.
- **Determine the origin at the exec site, never from the error's type.** `proc.MarkUserToolExit` marks the five places that run a third-party tool the user asked for. lstk shells out for its own purposes too (`brew upgrade`, `az cloud list`, `aws s3api create-bucket`), and those exits read `exit status N` exactly like the user's.
- **Mark proxy commands with an explicit annotation** rather than reusing `DisableFlagParsing`, so one declaration governs both `proxied` and the existing `subcommand` derivation.
- **Supersede draft PR #499**, whose `result.proxy_error` boolean is removed. Its exec-site marker, its `azurecli` split, and its integration-environment fix are kept.
- Not changed: `exit_code` (already the wrapped tool's own, from #396), `error_msg`, `subcommand`, and every other event field.

## Non-goals

- **A reliability metric.** An lstk-attributed failure answers "did lstk fail to complete this invocation", and most of what lands there is the user's environment or input — no AWS CLI on PATH, Docker down, a mistyped account id. Separating those needs a *why* axis. The repo already has one (`output.ErrorCategory`), but it is unreachable from telemetry today; wiring it in is a separate ticket, noted in design.md's Risks.
- **Fixing the denominator's known holes.** Invocations that fail in `PreRunE` emit no event, and `lstk completion bash` emits a guaranteed success on every shell startup. Both are pre-existing, both bound how precisely any rate can be read, and both are documented in the contract rather than fixed here.
- **Changing any exit code.** Some lstk-owned failures propagate a child's exit code today; design.md Decision 6 explains why that is left alone.
- **A client-side `error_source` enum.** Considered and rejected in design.md Decision 1: the pipe must derive it for pre-cutover rows regardless, so emitting it as well would give the column two sources of truth.

## Capabilities

### Added Capabilities

- `command-telemetry`: requirements for what an `lstk_command` event records about an invocation's population, its wrapped tool's exit, and its interruption, so error rates can be computed per population and rankings can separate lstk's failures from wrapped tools' and from cancellations.

## Impact

- **Touched code**: `internal/telemetry/events.go` (three fields and a struct-shaped `EmitCommand` argument — the positional list would otherwise reach ten), `internal/proc/exit.go` (the marker, from #499), `cmd/root.go` (the annotation, the result builder beside `ExitCode`, the emit call), `cmd/extension.go`, `cmd/{aws,az,cdk,sam,terraform}.go` (annotation), `internal/{awscli,azurecli,extension}` and `internal/iac/{cdk,sam,terraform}/cli` (marker at the exec sites, from #499).
- **Tests**: `internal/proc/exit_test.go`, `internal/telemetry/events_test.go`, `cmd/instrument_test.go`, `test/integration/telemetry_test.go`, `test/integration/extension_test.go`, `test/integration/signal_forwarding_test.go`.
- **Extensions**: `internal/extension.Invoke` no longer marks its exit (PR #499's draft did); `dispatchExtension` emits `proxied: false`.
- **Docs**: none user-facing. No command, flag, output, env var, or documented behavior changes — this is the internal analytics payload, which nothing under `docs/` describes. `CLAUDE.md`'s "Attributing Wrapped-Tool Exits" paragraph is updated to name the new fields.
- **Consumers**: `fct_lstk_command` (repo `localstack/localstack-dwh`) unpacks three more keys and derives `proxied`, `tool_ran`, and `error_source`; the two Grafana panels swap their failure predicate. The contract, the per-row cutover marker, and the retroactive rule are specified in design.md. The dashboard work is outside this repo.
- **Coordination**: `proxied` matches what DEVX-1004 specifies; `proxy_exit_code` replacing the `proxy_error` boolean is what the DevX Weekly of 2026-09-21 converged on; `cancelled` is the one addition, justified in design.md Decision 5.
