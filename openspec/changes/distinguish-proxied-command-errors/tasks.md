TDD throughout: each test task precedes its implementation task and must be run and seen to fail for the stated reason before the code lands. Kept from the earlier `proxy_error` revision of this change: the exec-site marker (`internal/proc/exit.go` + tests), the five marked third-party sites, the `azurecli` Exec/Run split, and the `test/integration/env/env.go` fix. Dropped from it: the mark on `extension.Invoke` (extensions are lstk's own code).

## 1. Event schema

- [x] 1.1 Unit test (`internal/telemetry/events_test.go`): a zero-value result marshals `parameters.proxied: false` and `result.cancelled: false`, and omits `result.proxy_exit_code`; a result with `ProxyExitCode` pointing at `0` emits `"proxy_exit_code": 0`; `proxy_error` is gone — fails because the fields do not exist
- [x] 1.2 Convert `EmitCommand` to take a `CommandEvent`-shaped struct rather than positional arguments (the list would otherwise reach ten; two call sites). Add `CommandParameters.Proxied` (bool, not omitempty), `CommandResult.ProxyExitCode` (`*int`, omitempty — absent, never `null`), `CommandResult.Cancelled` (bool, not omitempty); remove `ProxyError`. Document the presence semantics on `ProxyExitCode`

## 2. Proxy-command annotation

- [x] 2.1 Unit test (`cmd/instrument_test.go`): the set of commands in the real tree carrying `proxyCommandAnnotation` is exactly `{aws, az, cdk, sam, terraform}`; `az start-interception`, `az stop-interception`, and `setup azure` are not in it — fails because the annotation does not exist
- [x] 2.2 Add `proxyCommandAnnotation` to `cmd/root.go` beside the existing constants; set it on the five proxies; switch `instrumentCommands`' `subcommand` derivation from `c.DisableFlagParsing` to the annotation (behavior-neutral, see design.md Decision 4)

## 3. Result builder

- [x] 3.1 Unit test (`cmd/instrument_test.go`): the builder over the matrix — not proxied + nil; not proxied + plain error; proxied + nil (`proxy_exit_code` 0); proxied + marked exit 252 (`proxy_exit_code` 252, `exit_code` 252); proxied + unmarked `*exec.ExitError` (no `proxy_exit_code`); proxied + plain error (none); any + done context (`cancelled`); any + `context.Canceled` wrapped in `SilentError` (`cancelled`); nil + done context (`cancelled: true, exit_code: 0`, raw)
- [x] 3.2 Add `commandResult(ctx, err, proxied)` beside `cmd.ExitCode` in `cmd/root.go`; wire it into `instrumentCommands` (proxied from the annotation) and `dispatchExtension` (proxied `false`: extensions are lstk's own code). Record on it why the child's exit code is not consulted for cancellation and why `nil` on a proxied invocation reads as the tool exiting 0
- [x] 3.3 Correct `cmd.ExitCode`'s doc comment: lstk's own failures do not all collapse to 1 (`sam`/`cdk` version probes, terraform's S3 provisioning, `lstk update`'s `brew`)

## 4. End-to-end coverage (`test/integration`)

Rewrite the four `proxy_error` tests from #499 rather than adding beside them; all `t.Parallel()`, none starts a container.

- [x] 4.1 Fake `aws` exiting 252 → `proxied: true`, `proxy_exit_code: 252`, `exit_code: 252`, `cancelled: false`
- [x] 4.2 `lstk aws` failing preflight (`unreachableDockerHost`) → `proxied: true`, no `proxy_exit_code` key, `exit_code: 1`
- [x] 4.3 `lstk az stop-interception` failing → `proxied: false`, no `proxy_exit_code`, command `az stop-interception`
- [x] 4.4 `lstk az group lst` passthrough exiting 3 → `proxied: true`, `proxy_exit_code: 3`
- [x] 4.5 **Successful** proxy invocation (fake `aws` exiting 0 against `awsHealthServer`) → `proxied: true`, `proxy_exit_code: 0`, `exit_code: 0` — the case that makes the denominator computable
- [x] 4.6 `lstk start` against a pre-started container (extend `TestStartCommandSendsTelemetryEvent`) → `proxied: false`, no `proxy_exit_code`
- [x] 4.7 Extension exiting 7 (`extension_test.go`) → `proxied: false`, no `proxy_exit_code`, `exit_code: 7` — extensions are lstk's own code
- [x] 4.8 Signalled child (`signal_forwarding_test.go`, `!windows`): SIGTERM to `lstk sig signal-wait` → `cancelled: true`, `exit_code: 41` — the child traps the signal and exits normally, so this is the case an exit-code test would misclassify

- [x] 4.9 Interactive Ctrl-C on `lstk aws` in a PTY (fake `aws` with `trapExitCode: 130`, `startLstkInPTY`, `!windows`) → `cancelled: true`, `proxy_exit_code: 130` — found by manual testing; lstk never receives the SIGINT on this path, so `proc.RunInPTY` marks the forwarded ETX byte (`proc.WasInterrupted`)

## 4b. Error code (the why axis)

- [x] 4b.1 Unit tests: `output.Fail` carries the code on the returned `SilentError`; `ErrorCodeOf` sees through `%w` and `ExitCodeError`; `CommandResult` emits `error_code`/`error_category` only when set; `commandResult` reads them
- [x] 4b.2 Plumbing: `SilentError.Code`, `output.Fail`, `output.ErrorCodeOf`, the two fields, the builder read
- [ ] 4b.3 Integration tests: `lstk aws --account 123 s3 ls` → `VALIDATION_ERROR`/`USAGE`; Docker down → `RUNTIME_UNAVAILABLE`/`RUNTIME`
- [ ] 4b.4 Convert sites to `output.Fail`: `emitValidationError` (new code `VALIDATION_ERROR`), the five proxies' `runtime not healthy` preflight, `HandleNoRunningContainer` (new code `EMULATOR_NOT_RUNNING`), the wrong-emulator and az-not-installed preflights, and the sites that already set a `Code` but returned a bare `SilentError`
- [ ] 4b.5 Follow-up ticket: classify the remaining `ErrorEvent` sites; add the pipe coverage query

## 5. Docs in this repo

- [x] 5.1 Update `CLAUDE.md` "Attributing Wrapped-Tool Exits" to name `proxied` / `proxy_exit_code` / `cancelled` instead of `proxy_error`
- [x] 5.2 Update `openspec/changes/distinguish-proxied-command-errors/specs/command-telemetry/spec.md` (done alongside this file)

## 6. Optional, pending review

- [ ] 6.1 Add `syscall.SIGHUP` to `main.go`'s `signal.NotifyContext` set so a closed terminal still emits (changes shutdown behavior; call out, do not fold in)

## 7. Hand-off

- [ ] 7.1 Post design.md's Analytics contract on DEVX-1004 and open a PR against `localstack/localstack-dwh` `fct_lstk_command.pipe` (the `unpacked` additions, the `classified` `proxied`/`error_source` rules, a dq check that `has_origin = 1 AND proxied = 0 AND tool_ran = 1` never occurs — a tool exit on a non-proxied row; note `tool_ran = 0 AND proxy_exit_code != 0` can never fire because `JSONExtractInt` reads a missing key as 0)
- [ ] 7.2 Update the two Grafana panels (15, 19) to `error_source = 'lstk'` and the `proxied = 0` denominator; add the proxied-exits panel
- [ ] 7.3 Open a follow-up to plumb `output.ErrorCode`/`ErrorCategory` into the command event (the why axis)
- [ ] 7.4 Open a follow-up for the invocations that emit no event at all — `PreRunE` failures, flag-parse errors, unresolved extension names
- [x] 7.5 PR #499 carries this change (same branch as the earlier `proxy_error` revision)
