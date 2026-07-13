## Why

Scripts and other tools that drive `lstk` today have to parse human-readable text output, the same problem `--non-interactive` already solves for *input* (no prompts, no TTY assumptions). There is no equivalent signal on the *output* side telling a command "render as JSON instead of prose", and no way for that signal to reach `lstk` extensions the way `nonInteractive` already does. This change adds the flag and threads it everywhere `--non-interactive` already goes, without requiring every command to implement JSON rendering yet — that follows command-by-command in later changes.

## What Changes

- Add a persistent `--json` boolean flag on the root command, recognized the same way `--non-interactive` is: parsed before the config/log/UI stack initializes, available to every built-in command and to the extension dispatch path.
- Extend `internal/extension.Context` with a `json` field (JSON-encoded as `"json"`, following the existing `nonInteractive` field), populated at the `dispatchExtension` command boundary and rendered into `LSTK_EXT_CONTEXT`, so extensions can decide their own output format the same way they already read `nonInteractive`.
- Expose the resolved flag to command implementations via `internal/env.Env` (mirroring `NonInteractive`) so any command's `RunE` can read it without re-deriving it from Cobra flags.
- `--json` implies non-interactive rendering for that invocation: a command that honors `--json` must not launch the Bubble Tea TUI or block on stdin, even on a TTY. This change only wires the flag through; only `internal/ui.IsInteractive`/`isInteractiveMode` gains the `--json` short-circuit — individual commands still render plain-text output until they're updated in follow-up work to check the flag and emit JSON.
- No command changes its actual output format in this change. That is explicitly deferred, command by command, to future work.
- A command that has not been marked as JSON-capable SHALL reject `--json` with an error naming the command, instead of silently accepting the flag and rendering plain text anyway. JSON capability is an explicit per-command opt-in (an annotation), so today every built-in command rejects `--json` — none has opted in yet. This rejection uses lstk's established interactive error style (a title plus a `See help: lstk -h` action), matching the format already used elsewhere (e.g. `dispatchExtension`'s unknown-command error) instead of a bare title.
- `lstk` extension dispatch is exempt from this rejection: an unknown command that resolves to an extension is not "a command lstk owns" in this sense, so it is never gated — the extension receives `json: true` in its context and decides for itself whether it honors the flag.
- Proxy commands (`aws`, `terraform`, `cdk`, `sam`, `az`) are rejected via this same mechanism, but only when `--json` appears *before* the proxy command's own name (`lstk --json aws ...`) — the same flag-namespace slot `--non-interactive`/`--config` already occupy there. `--json` appearing anywhere from the command name onward (including the wrapped tool's own subcommand/arguments) is left completely untouched and forwarded verbatim: that part of the invocation is the wrapped tool's own business, and Terraform already has a real `-json`/`--json` flag there (`terraform plan -json`, `terraform apply -json`, etc. — lstk's own `internal/iac/terraform/cli` package already shells out to `terraform version -json` and `terraform providers schema -json`), so intercepting and rejecting it in that position would break a legitimate, already-real user request.

## Capabilities

### New Capabilities
- `json-flag`: recognizing a global `--json` flag, exposing its resolved value to command implementations and to `lstk` extensions, forcing non-interactive rendering when set, and rejecting the flag — using lstk's interactive error style — for any command that hasn't implemented JSON output. For proxy commands (`aws`, `terraform`, `cdk`, `sam`, `az`) this rejection applies only to `--json` typed before the command's own name; from the name onward `--json` is always forwarded to the wrapped tool unmodified, since that part of the invocation is never lstk-rendered output. Does not cover any individual command emitting JSON — that is out of scope here.

### Modified Capabilities
(none — no existing spec covers global flags or extension context yet)

## Impact

- `cmd/root.go`: register the persistent `--json` flag next to `--non-interactive`; add a `jsonSupported` command annotation constant and a `requireJSONSupport` tree-walker (alongside `instrumentCommands`/`wrapCommandsWithTracing`) that rejects `--json` for any command lacking the annotation, except the extension-dispatch branch of the root command. Its `output.ErrorEvent` gains a `See help: lstk -h` action, matching the interactive style used elsewhere. No proxy-command carve-out is needed in this walker itself — it reacts to `cfg.JSON` the same way regardless of what set it.
- `cmd/proxy.go`: `stripGlobalFlags` still does not recognize `--json` (it operates on the position-blind blob Cobra hands to `PreRunE`, covering everything from the command name onward). A new helper, mirroring `rejectPreSubcommandFlags`'s raw-`os.Args` scanning technique, recognizes `--json`/`--json=<value>` only in the window before the resolved proxy command's own name/alias.
- `cmd/aws.go`, `cmd/terraform.go`, `cmd/cdk.go`, `cmd/sam.go`, `cmd/az.go`: each `PreRunE` gains a call to the new pre-name `--json` check, setting `cfg.JSON` when it resolves `true`. `az.go` gets this addition too, even though it still doesn't call `stripGlobalFlags` for `--non-interactive`/`--config` (that remains a separate, pre-existing, out-of-scope gap) — `--json` is deliberately made uniform across all five from the start.
- `internal/env/env.go`: add `JSON bool` to `Env`.
- `internal/extension/context.go`: add `JSON bool` (`json:"json"`) to `Context`; bump nothing (additive field, no `APIVersion` bump per the existing contract).
- `cmd/extension.go`: populate `Context.JSON` in `dispatchExtension`.
- `cmd/root.go` (`isInteractiveMode`): `--json` forces non-interactive mode.
- Docs: `docs/extensions-authoring.md` gains the new context field; CLAUDE.md's Infrastructure as Code Commands section should note the pre-name/post-name split for `--json` on proxy commands.
- No changes to `internal/output` event/sink types in this change — that lands when individual commands adopt JSON rendering.
