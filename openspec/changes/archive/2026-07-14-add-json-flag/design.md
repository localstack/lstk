## Context

`lstk` already has a global `--non-interactive` flag (`cmd/root.go:84`) that flows through three independent paths:

1. **Built-in commands** — a `bool` field on `env.Env`, set as the persistent flag's bind target, read via `isInteractiveMode(cfg)` (`cmd/root.go:398`).
2. **Proxy commands** (`aws`, `terraform`, `cdk`, `sam`, `az`) — these set `cobra.Command.DisableFlagParsing = true` so they can forward arbitrary args to the wrapped binary verbatim. `stripGlobalFlags` (`cmd/proxy.go`) manually scans `os.Args`-derived arg slices for `--non-interactive`/`--non-interactive=...`/`--config`/`--config=...`, removes them, and returns a `globalFlags` struct the proxy command applies to `cfg`. (`az` is the one exception: it doesn't call `stripGlobalFlags` at all, a pre-existing gap — see Decision 3.)
3. **Extensions** — `internal/extension.Context.NonInteractive` (`internal/extension/context.go:56`) is populated in `dispatchExtension` (`cmd/extension.go:61`) from `!isInteractiveMode(cfg)` and rendered into the `LSTK_EXT_CONTEXT` env var an extension process reads.

`--json` needs similar treatment for paths 1 and 3, and a *position-aware* variant of path 2 — see Decision 3. The one behavioral difference from `--non-interactive` on paths 1 and 3: `--json` isn't just a value commands can inspect — it must also *force* non-interactive rendering, because JSON output and an interactive Bubble Tea TUI are mutually exclusive. `--non-interactive` has no such side effect on itself (it *is* the interactivity switch); `--json` piggybacks on it.

This change stops at making the flag observable everywhere `nonInteractive` is observable (for built-ins and extensions). No command changes its emitted output. Command-by-command JSON rendering is deferred (see proposal "Impact").

A silent gap in that framing: if nothing rejects `--json` on an unimplemented command, the user's explicit request is simply ignored and plain text comes out anyway, indistinguishable from a bug. So this change also adds a rejection path — every *lstk-owned* command must explicitly opt in to `--json` support (none do yet), and an unsupported command errors out by name instead of pretending to comply. Proxy commands turn out to need this same rejection too, but only for the slot that is unambiguously lstk's own: `--json` typed *before* the proxy command's own name (`lstk --json aws ...`) sits in exactly the position `--non-interactive`/`--config` already occupy there, so a user typing it means it for lstk, and lstk should say so clearly rather than silently forwarding a flag the wrapped tool may not understand. `--json` typed anywhere from the command name onward remains the wrapped tool's own business (see Decision 3) — that part of the reasoning from the original design still holds.

## Goals / Non-Goals

**Goals:**
- `--json` is recognized by the root command and every built-in subcommand (persistent flag, like `--non-interactive`).
- `--json` is exposed on `env.Env` so any command's `RunE` can branch on it later without re-parsing flags.
- `--json` is conveyed to extensions via `extension.Context`/`LSTK_EXT_CONTEXT`, additively (no `APIVersion` bump).
- Setting `--json` forces `isInteractiveMode` to return `false`, so no command can accidentally launch the TUI while `--json` is set.
- `docs/extensions-authoring.md` documents the new context field for extension authors.
- A command that has not opted in to JSON support rejects `--json` with an error that names the command, rather than silently rendering plain text.
- That rejection uses lstk's established "interactive style" for actionable errors: a `Title` plus a `See help: lstk -h` action line, matching the format already used by e.g. `dispatchExtension`'s unknown-command error and `requireRunningAWSEmulator` — not just a bare title.
- Extension dispatch is exempt from this rejection entirely; extensions are not gated.
- Proxy commands (`aws`, `terraform`, `cdk`, `sam`, `az`) are rejected the same way as any other unsupported command **when `--json` appears before the proxy command's own name** (`lstk --json aws ...`) — that slot is unambiguously lstk's own flag namespace, the same one `--non-interactive`/`--config` already occupy there. `--json` appearing anywhere from the command name onward is left completely alone and forwarded to the wrapped tool untouched, exactly as before — see Decision 3.

**Non-Goals:**
- No command emits actual JSON in this change. `internal/output` gains no new sink/event types here.
- No validation that `--json` and `--non-interactive` don't conflict — they don't; `--json` is a strict superset (forces non-interactive, adds nothing to the interactive/non-interactive axis itself).
- No change to telemetry (`tel.EmitCommand`) beyond the flag already being picked up generically by the existing `c.Flags().Visit` loop in `instrumentCommands` (`cmd/root.go:339`) — `--json` shows up there for free once it's a registered flag. (This does not apply to proxy commands: neither the pre-command-name detection nor anything after it is ever parsed by Cobra as a flag.)
- No JSON Schema / versioned output contract for future per-command JSON — left to the follow-up work that implements it.
- No fix for `az.go`'s `az <args>` passthrough not calling `stripGlobalFlags` for `--non-interactive`/`--config` (see Decision 3) — that's a pre-existing gap, out of scope here. `--json` itself no longer shares that gap: it gets its own, uniform, position-aware treatment across all five proxy commands including `az` (see Decision 3).
- No hand-rolled JSON error body for the rejection error itself — it renders as a normal plain-text `output.ErrorEvent` on stderr, same as every other pre-dispatch error (e.g. the unknown-command case in `dispatchExtension`), even though the user asked for `--json`. Introducing a minimal JSON error envelope before any real JSON sink exists is left to the work that adds the first real one.
- No attempt to detect a wrapped tool's own machine-readable-output flag (e.g. Terraform's `-json`) and auto-suppress lstk's own spinner/decorations on its behalf. `--non-interactive` already covers that need explicitly; inferring it from tool-specific flags would be fragile and is left as a possible future enhancement, not part of this change.

## Decisions

**1. Add `JSON bool` to `env.Env`, mirroring `NonInteractive`, not a separate global-flags struct.**
`env.Env` is already the place `cfg.NonInteractive` lives and is threaded everywhere (`buildStartOptions`, command `RunE`s, `isInteractiveMode`). Adding a sibling field keeps every consumer's existing `cfg` plumbing unchanged — no new parameter to thread through constructors.
*Alternative considered*: a dedicated `OutputMode` enum (`plain`/`json`/`interactive`) on `Env`. Rejected for this change because it would require touching `isInteractiveMode` call sites beyond what's needed and because the proposal explicitly keeps command-level rendering out of scope — a bool is the minimal faithful mirror of `NonInteractive` today, and an enum can replace it later if a third mode ever appears.

**2. `--json` forces non-interactive by changing `isInteractiveMode`, not by setting `cfg.NonInteractive = true` as a side effect of flag parsing.**
`isInteractiveMode(cfg)` becomes `!cfg.NonInteractive && !cfg.JSON && ui.IsInteractive()`. This keeps `NonInteractive` and `JSON` as independently truthful, inspectable fields — a command checking `cfg.NonInteractive` still gets an honest answer to "did the user pass `--non-interactive`", while `isInteractiveMode` remains the single place that answers "should I render interactively right now."
*Alternative considered*: setting `cfg.NonInteractive = true` in `PreRunE` when `--json` is set. Rejected — it would make `cfg.NonInteractive` lie about what the user actually typed, which future code (or telemetry reading flags) could misinterpret.

**3. `--json` is recognized ONLY when it appears before the proxy command's own name (`lstk --json aws ...`), for all five proxy commands (`aws`, `terraform`, `cdk`, `sam`, `az`) uniformly. `--json` anywhere from the command name onward is never touched — always forwarded to the wrapped tool.**

No new error-message code is needed: `requireJSONSupport` (Decision 6) already wraps every command's `RunE`, proxy commands included, and none of the five carry `jsonSupportedAnnotation` — so once `cfg.JSON` is `true`, it rejects with the exact same message a built-in command gets. `cfg.JSON` simply never had a path to becoming `true` for these commands before; this decision adds the one specific path that should set it (the pre-name scan), without resurrecting the over-broad one (`stripGlobalFlags`) that also caught the after-name position and broke Terraform.

`az` is included from the start here, rather than inheriting `--json` treatment by accident the way it does for `--non-interactive`/`--config`. Those two remain a separate, pre-existing, out-of-scope gap (`az.go` never calls `stripGlobalFlags` at all) — but `--json` gets its own explicit pre-name check added to `az.go` alongside the other four, so all five are uniform for `--json` specifically even though they remain non-uniform for the older flags.

*Alternative considered*: scope the pre-name check to `terraform`/`cdk`/`sam` only (the `cmd/iac.go` family), leaving `aws`/`az` on iteration-2 behavior. Rejected — `aws` has no leading-flag tier of its own to conflict with (no `--region`/`--account` equivalent), so there's no reason its pre-name slot should behave differently from terraform's, and leaving `az` as the one exception again reintroduces the exact asymmetry iteration 2 was trying to remove.

**4. `extension.Context` gets `JSON bool` with tag `json:"json"`, unconditionally present (no `omitempty`), matching `NonInteractive`'s treatment.**
Per the existing doc comment in `context.go`, fields added after v1 must still be *distinguishable when absent* for old extensions to detect them via presence-check — but `bool` with no `omitempty` always serializes (`false` when unset), same as `NonInteractive` today. This is consistent with how `NonInteractive` was added previously (no version bump), and `az` continues to work because `APIVersion` is unchanged.

**5. `isInteractiveMode`'s new dependency on `cfg.JSON` also affects `dispatchExtension`'s `Context.NonInteractive` computation (`!isInteractiveMode(cfg)`).**
Once `--json` is set, `isInteractiveMode` returns `false`, so `Context.NonInteractive` becomes `true` automatically for extensions too — an extension already gets the correct combined signal (`nonInteractive: true, json: true`) without extra wiring beyond setting `Context.JSON` from `cfg.JSON` directly.

**6. JSON support is an explicit per-command opt-in via a Cobra annotation (`jsonSupportedAnnotation`), enforced by a fourth tree-walker (`requireJSONSupport`) added to the same wrapping chain as `instrumentCommands`/`wrapCommandsWithTracing` in `Execute()`.**
This mirrors the existing `canonicalCommandAnnotation` convention (`cmd/root.go:35`) and the existing walker shape exactly: build the tree in `NewRootCmd`, then walk it, wrapping every leaf `RunE`. `requireJSONSupport` checks, before calling the original `RunE`, whether `cfg.JSON` is set and the command's `Annotations[jsonSupportedAnnotation]` is absent; if so it emits an `output.ErrorEvent` (title includes the command's resolved name, message "this command is not able to provide output in JSON format") via `output.NewPlainSink(os.Stderr)` and returns `output.NewSilentError(...)` without calling `original`. It reuses the exact same root/extension-dispatch carve-out `instrumentCommands` already has (`c == c.Root() && len(args) > 0` → skip) so extension dispatch is never gated. Because zero commands set the annotation today, this walker's practical effect right now is "every built-in command rejects `--json`" — which is the correct starting state, and each future per-command JSON change adds one line (the annotation) to lift it.
It is wired into `Execute()` *before* `instrumentCommands`/`wrapCommandsWithTracing` are applied (i.e. `requireJSONSupport(root, cfg)` runs first, then those two wrap the result), so a rejected invocation still shows up in telemetry as a normal command error (exit code 1, the rejection message) rather than being invisible to it.
*Alternative considered*: an explicit allow-list (`map[string]bool` of command paths) checked in one place. Rejected — an annotation on the command itself is discoverable at the point the command is defined (same place `Short`/`Long`/`RunE` are set), whereas a separate list drifts from the commands it describes as new ones are added; the codebase already prefers the annotation pattern for exactly this kind of cross-cutting command metadata.
This walker no longer needs a proxy-command carve-out (Decision 3 covers why `cfg.JSON` can become `true` for these commands now, in exactly one position), and still needs no code change of its own to handle that case — the same `if cfg.JSON { ... }` check fires regardless of *how* `cfg.JSON` became `true`.

**7. `requireJSONSupport`'s `ErrorEvent` gains `Actions: []output.ErrorAction{{Label: "See help:", Value: "lstk -h"}}`, matching lstk's established interactive error style.**
`formatErrorEvent` (`internal/output/plain_format.go`) already renders `Title` plus each `Actions` entry as `  ==> Label Value`; `dispatchExtension`'s unknown-command error and `requireRunningAWSEmulator` (`cmd/iac.go`) already use exactly this `{Label: "See help:", Value: "lstk -h"}` action for the equivalent "you asked for something lstk can't do here" case. `requireJSONSupport` was the odd one out with a bare `Title` and no action line. Since every JSON-unsupported rejection — built-in commands and, per Decision 3, proxy commands typing `--json` before their name — funnels through this one `Emit` call, this is a single-line fix that applies everywhere at once:
```
Error: "snapshot load" is not able to provide output in JSON format
  ==> See help: lstk -h
```
*Alternative considered*: a proxy-command-specific error with a different action (e.g. pointing at the proxy command's own `--help`). Rejected — there's exactly one rejection message in the system now; giving proxy commands a bespoke variant would reintroduce the duplication Decision 3 deliberately avoided by routing through the existing gate.

## Risks / Trade-offs

- **[Risk]** Adding `--json` to the root's persistent flags means it appears (and is technically settable) on commands that will never implement JSON rendering (e.g. `lstk version`), giving a false impression of support → **Mitigation**: this is the same trade-off `--non-interactive` already makes (it's persistent/global, not opt-in per command); acceptable since command-level adoption is explicit future work and can validate/ignore the flag per command as needed.
- **[Risk]** Extensions written against `APIVersion` 1 won't know to look for `json` in the context payload → **Mitigation**: by design, additive fields are meant to be discovered via presence, not version bump; this is documented behavior extension authors already rely on for `nonInteractive`.
- **[Risk]** A user who types `lstk aws --json ...`/`lstk terraform --json ...` (after the command name) expecting *lstk* to render JSON still gets no feedback that lstk ignored the flag — it silently reaches the wrapped tool instead, which may accept it (Terraform), reject it as unknown (aws, cdk, sam today), or ignore it → **Mitigation**: this is now strictly narrower than before Decision 3's revision — the much more likely typo/habit case (`--json` typed *before* the command name, the same place `--non-interactive` goes) is caught and rejected with a clear message. The remaining gap is the *after*-name position specifically, where an lstk-level error would be actively wrong for Terraform (which understands `--json` legitimately there) and there is no reliable way to distinguish intent without per-tool flag knowledge lstk doesn't have. The wrapped tool's own error (or success) is the correct, honest feedback for that position.
- **[Risk]** The rejection error itself is plain text, not JSON, even though it's the direct response to a `--json` request → **Mitigation**: accepted for this change (see Non-Goals); every other pre-dispatch lstk error works the same way, and inventing a one-off JSON error envelope before any real sink exists would be speculative.
- **[Risk]** The pre-command-name `--json` scan (Decision 3) reads raw `os.Args`, so — like `rejectPreSubcommandFlags` before it — it cannot distinguish the literal command token from an identically-valued flag argument earlier on the line (e.g. `--config aws` before the real `aws`) → **Mitigation**: an inherited, already-accepted limitation of the technique this reuses, not a new one; not worth solving here.

## Migration Plan

No migration — purely additive flag and context field, default `false`/absent-equivalent, no behavior change for anyone not passing `--json`.

## Open Questions

None — scope is intentionally narrow (flag plumbing plus its rejection path); command-level JSON rendering design is deferred to the changes that implement it per command.
