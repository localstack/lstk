## 1. Core flag plumbing

- [x] 1.1 Add `JSON bool` to `internal/env.Env` (`internal/env/env.go`), next to `NonInteractive`.
- [x] 1.2 Register the persistent `--json` flag on the root command in `cmd/root.go`, bound to `cfg.JSON`, next to the existing `--non-interactive` registration.
- [x] 1.3 Update `isInteractiveMode` in `cmd/root.go` to also return `false` when `cfg.JSON` is `true` (`!cfg.NonInteractive && !cfg.JSON && ui.IsInteractive()`).

## 2. JSON-support rejection gate

- [x] 2.1 Add a `jsonSupportedAnnotation` constant in `cmd/root.go`, next to `canonicalCommandAnnotation`.
- [x] 2.2 Implement `requireJSONSupport(root *cobra.Command, cfg *env.Env)` in `cmd/root.go`: walks the command tree like `instrumentCommands`, wraps each leaf `RunE`, and before calling the original, if `cfg.JSON` and the command's `Annotations[jsonSupportedAnnotation]` is absent, emits an `output.ErrorEvent` ("this command is not able to provide output in JSON format", command name included in the title) via `output.NewPlainSink(os.Stderr)` and returns `output.NewSilentError(...)` instead of calling `original` — except when `c == c.Root() && len(args) > 0` (extension dispatch), which is never gated.
- [x] 2.3 Wire `requireJSONSupport(root, cfg)` into `Execute()` (`cmd/root.go`), before `instrumentCommands(root, tel)` and `wrapCommandsWithTracing(root)` so a rejection still records as a normal command error in telemetry/tracing.

## 3. Proxy command --json passthrough

> **Corrected 2026-07-03**: the original 3.1–3.4 below made `aws`/`terraform`/`cdk`/`sam` intercept and reject `--json` anywhere in the invocation, mirroring `--non-interactive`. That broke a real case: Terraform already has its own `-json`/`--json` flag (`terraform plan -json`, etc. — lstk's own `internal/iac/terraform/cli` package already shells out to `terraform version -json`), so lstk was silently swallowing and rejecting a legitimate Terraform request regardless of where `--json` appeared. The fix below (3.1–3.3) made `stripGlobalFlags` stop recognizing `--json` at all, for all five proxy commands uniformly.
>
> **Corrected again — see section 8**: that second iteration overcorrected — it also lost lstk-level feedback for `lstk --json aws ...` (the pre-command-name slot `--non-interactive`/`--config` already use), which just silently reached the wrapped tool. Section 8 adds back a *position-aware* rejection: `--json` before the proxy command's own name is rejected exactly like an unsupported built-in command; `--json` from the command name onward (where 3.1–3.3 below apply) stays untouched. `stripGlobalFlags` itself still does not recognize `--json` — 3.1–3.3 remain correct as the "everything from the command name onward" half of the final design.

- [x] 3.1 Remove the `json` field and the `--json`/`--json=<value>` cases from `globalFlags`/`stripGlobalFlags` in `cmd/proxy.go` — `--json` must fall through untouched into the forwarded args, like any arg lstk doesn't recognize.
- [x] 3.2 Remove the `if gf.json { cfg.JSON = true }` lines added to `cmd/aws.go`, `cmd/terraform.go`, `cmd/cdk.go`, and `cmd/sam.go`'s `PreRunE` — `stripGlobalFlags`/the post-command-name blob must never set `cfg.JSON` (see section 8 for the separate pre-command-name check that now does).
- [x] 3.3 `cmd/az.go`'s `az <args>` passthrough needs no change *for this section* — it already never intercepts any flag from `stripGlobalFlags`'s perspective. (It does gain a change in section 8, for the new pre-command-name `--json` check that now applies to all five proxy commands.)

## 4. Extension context

- [x] 4.1 Add `JSON bool` with tag `json:"json"` to `extension.Context` in `internal/extension/context.go` (no `APIVersion` bump; update the field's doc comment referencing `NonInteractive` to also mention `JSON`).
- [x] 4.2 Populate `Context.JSON` from `cfg.JSON` in `dispatchExtension` (`cmd/extension.go`).

## 5. Tests

- [x] 5.1 Unit test: `stripGlobalFlags` does NOT recognize `--json` or `--json=<value>` in any position — they pass through unmodified in the returned args, and `globalFlags` has no `json` field. (Replaces the earlier version of this test, which asserted the opposite.)
- [x] 5.2 Unit test: `isInteractiveMode` returns `false` when `cfg.JSON` is `true`, even if `cfg.NonInteractive` is `false` and a TTY is present.
- [x] 5.3 Integration test: `lstk <command> --json` does not launch the TUI on a PTY (reuse the existing PTY test harness pattern).
- [x] 5.4 Integration test: an extension invoked with `--json` receives `"json": true` (and `"nonInteractive": true`) in `LSTK_EXT_CONTEXT`; invoked without it receives `"json": false`; the extension still runs (not gated).
- [x] 5.5 Integration test: an unannotated built-in command (e.g. `lstk status --json`) exits non-zero with an error naming "status" and stating JSON output isn't supported, and performs none of its normal work.
- [x] 5.6 Integration test: `lstk --json` (no subcommand) is rejected the same way, naming "start".
- [x] 5.7 Integration test: `lstk aws --json s3 ls` (and the terraform/cdk/sam/az equivalent, `--json` immediately after the command name) is **forwarded, not rejected** — `--json` reaches the (stubbed/absent) wrapped binary verbatim, lstk's own "not able to provide output in JSON format" error never appears, and `env.Env.JSON` stays `false`. Also cover `--json` after the wrapped tool's own action (e.g. `lstk terraform plan --json`) for the same forwarding behavior. (Narrowed from the previous version of this test, which also covered the *before*-command-name position — that position now rejects; see 8.3.)
- [x] 5.8 Consolidate the per-command forwarding checks (5.7) into one parametrized test over `aws`/`terraform`/`cdk`/`sam`/`az` rather than treating `az` as a documented exception to the other four.

## 6. Docs

- [x] 6.1 Document the new `json` field in `docs/extensions-authoring.md`.
- [x] 6.2 Update CLAUDE.md's Extensions section (`LSTK_EXT_CONTEXT` field list) to mention `json`.
- [x] 6.3 Update CLAUDE.md's Infrastructure as Code Commands section to describe the pre-name/post-name split: `--json` before a proxy command's own name is rejected exactly like an unsupported built-in command; `--json` from the command name onward is forwarded to the wrapped tool untouched — mentioning Terraform's own `-json`/`--json` flag as the reason the post-name position must stay hands-off (unlike `--non-interactive`/`--config`, which lstk does capture for `aws`/`terraform`/`cdk`/`sam` before the command name, or anywhere in `az`'s case per its own pre-existing gap).

## 7. Interactive-style error for JSON-unsupported rejections

- [x] 7.1 Add `Actions: []output.ErrorAction{{Label: "See help:", Value: "lstk -h"}}` to the `output.ErrorEvent` `requireJSONSupport` emits (`cmd/root.go`), matching the interactive style already used by `dispatchExtension`'s unknown-command error and `requireRunningAWSEmulator` (`cmd/iac.go`). This applies to every JSON-unsupported rejection at once (built-in commands today; proxy commands once section 8 lands), since they all funnel through this one `Emit` call. Rendered form:
  ```
  Error: "snapshot load" is not able to provide output in JSON format
    ==> See help: lstk -h
  ```

## 8. Position-aware --json rejection for proxy commands

- [x] 8.1 Add a helper in `cmd/proxy.go` (name TBD, e.g. `jsonPrecedesCommandName`) that mirrors `rejectPreSubcommandFlags`'s technique (`cmd/iac.go`): find the index of `cmd.CalledAs()` in raw `os.Args`, then scan only `os.Args[1:cmdIdx]` (before that index) for `--json`/`--json=<value>`, with the same boolean-aware parsing `stripGlobalFlags` already has for `--non-interactive` (bare `--json` / `--json=true` → `true`; `--json=false` → `false`; a malformed value → `true`). Returns the resolved bool (or directly sets it on `cfg`).
- [x] 8.2 Call the new helper from each of `cmd/aws.go`, `cmd/terraform.go`, `cmd/cdk.go`, `cmd/sam.go`, and `cmd/az.go`'s `PreRunE`, setting `cfg.JSON = true` when it resolves `true`. `az.go`'s `PreRunE` currently only calls `initConfig(nil)` — this is the one new line it gains; its separate `--non-interactive`/`--config` gap (never calling `stripGlobalFlags`) is untouched. No changes are needed in `requireJSONSupport` itself: it already wraps every proxy command's `RunE` and already rejects whenever `cfg.JSON` is `true` and the command lacks `jsonSupportedAnnotation`, regardless of how `cfg.JSON` became `true`.
- [x] 8.3 Integration test: `lstk --json aws <args>` (and the terraform/cdk/sam/az equivalent) is **rejected** the same way as an unsupported built-in command, naming the proxy command, with the wrapped binary never invoked. Cover `--json=true` (rejected), `--json=false` (not rejected, wrapped tool runs), and a malformed value (rejected) — mirroring `TestStripGlobalFlags`'s existing boolean-value coverage for `--non-interactive`.
- [x] 8.4 Update the existing built-in rejection integration tests (`TestJSONFlagRejectsUnannotatedBuiltinCommand`, `TestJSONFlagRejectsDefaultStartBehavior`) and the new proxy rejection test (8.3) to assert the `==> See help: lstk -h` action line from task 7.1, not just the title text.
