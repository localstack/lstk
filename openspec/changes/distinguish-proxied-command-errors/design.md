## Context

`lstk_command` events already carry a wrapped tool's real exit code and the leading tokens of its subcommand (#396). What they do not carry is whose failure a failure was, so every consumer has to guess from the command name and the error string. That guess is why users' own AWS CLI mistakes rank as lstk errors (DEVX-1003).

An earlier revision of this change proposed `result.proxy_error`. Its central insight is correct and survives here unchanged: the origin of a failure cannot be read off the error's type. An `*exec.ExitError` appears whenever any child exits non-zero, and lstk runs children of its own — `lstk update` shells out to `brew`, `lstk setup azure` to `az cloud list`, `lstk terraform init` to `aws s3api create-bucket`. All produce an error reading `exit status N`. Only the call site knows whether the user asked for that process.

One field is not enough. A failure-origin marker describes failures, and an error *rate* also needs its denominator — which population an invocation belongs to, successes included. And interruptions need a home: a wrapped tool that traps Ctrl-C exits 1 or 130, indistinguishable from its own failure.

**The consumer, inspected.** Events land in `localstack_prod_telemetry.lstk_event` (repo `localstack/analytics-backend`), whose `payload` column is an opaque `String`. `fct_lstk_command` (repo `localstack/localstack-dwh`, `tinybird/endpoints/fct_lstk_command.pipe`) is an *endpoint pipe*, not a table: an `unpacked` node reads keys with `JSONExtract*` at query time, a `classified` node derives everything else (`error_bucket`, `lstk_version_class`). Both Grafana panels on DEVX-1004 query that endpoint with `has_result = 1 AND exit_code != 0 AND error_bucket != 'user cancelled'`. Three consequences drive the decisions below:

1. Adding a field is one line in the pipe. There is no schema migration and no ingestion default.
2. `JSONExtractInt` returns `0` for a missing key. The pipe already guards its one nullable int the same way (`has_result` via `JSONHas`), and a data-quality test documents the trap.
3. Because the raw JSON is kept, "did this client emit the new fields" is answerable per row with `JSONHas`, with no version comparison at all.

## Decisions

### Decision 1: Three raw fields; the interpretation lives in the pipe

```
parameters.proxied          bool   always emitted   the invocation asked for a wrapped tool
result.proxy_exit_code      int    only if the tool ran to completion; its own code, 0 included
result.cancelled            bool   always emitted   lstk's signal context was cancelled
result.error_code           str    only if the failing site classified it: the output.ErrorCode shown to the user
result.error_category       str    with error_code: its output.ErrorCategory (USAGE, RUNTIME, EMULATOR, ...)
```

The first three answer *who* failed; the last two answer *why*, and are what separates a user's `--account 123` from an lstk defect (Decision 8).

| `proxied` | `proxy_exit_code` | `cancelled` | Reading |
|---|---|---|---|
| false | absent | false | lstk's own command, extensions included; `exit_code` is lstk's verdict |
| true | absent | false | proxy invocation that failed before the tool ran (preflight, Docker down, tool not on PATH) — lstk's failure |
| true | `0` | false | tool ran and succeeded |
| true | `252` | false | tool ran and failed, with its real code |
| any | any | true | interrupted; not anybody's failure |

The pipe derives `error_source ∈ {none, lstk, proxy, cancelled}` from these (see Analytics contract).

**Why raw fields rather than a client-side `error_source` enum** (the shape an earlier revision of this design chose): the pipe has to derive `error_source` for every pre-cutover row anyway, from `command` and `error_msg`. Emitting the enum as well gives one column two sources of truth — client-emitted for new rows, SQL-derived for old — and two places for the rule to drift. With raw fields the rule exists once, in the `classified` node, beside `error_bucket` and `start_phase`, which is exactly where that pipe already keeps its interpretations. It is also what the DevX Weekly of 2026-09-21 asked for: "the raw data rather than the interpretation", and the exit code preserved per tool because future proxies will have tool-specific failure codes worth reading.

**Why `proxy_exit_code` in addition to `exit_code`, when they are equal whenever both exist**: `cmd.ExitCode` propagates the tool's code, so for a tool that ran and failed the two agree, and the value is redundant. What is not redundant is the *presence*: whether the wrapped tool ran at all, which nothing else carries. A boolean `tool_ran` would carry that alone; the int carries it and keeps the per-tool code in a column that lstk's own subprocess exits never populate — which is what answers the "shadowing" concern from the meeting (an lstk-owned `brew`/`sam` exit can no longer be mistaken for a proxied one) and what the meeting asked for ("both would be integers"). The value is raw: a tool killed by a signal it did not trap records Go's `-1` sentinel (Unix only; Windows reports the platform code).

**What it does not capture**: a tool that exits 0 after which lstk itself fails (a PTY teardown error, for instance) returns an unmarked non-exit error, so `proxy_exit_code` is absent and the row reads as lstk's failure with no tool exit. That is a true statement about whose failure it was; the lost information is only that the tool had finished. Marking successes at every exec site to recover it was judged not worth the plumbing for a path this rare.

**Why store `proxied` rather than derive it from `command`**: it *is* exactly derivable today (`command IN ('aws','az','cdk','sam','terraform')`, verified by walking the real Cobra tree — see Decision 4 for the traps). It is rejected as the going-forward mechanism because it puts the rule in the consumer, where a sixth proxy command silently files its successes and its preflight failures into the "lstk's own commands" population. A test can pin the annotated set in this repo (task 2.1) but cannot make the dashboard update. The derivation is retained only for pre-cutover rows.

**Alternatives rejected**: `proxied` + `proxy_error` booleans (#499's shape; no home for cancellation, and `proxy_error=false` conflates success with lstk failure); `proxy_exit_code` alone (absence would conflate "not proxied" with "proxy whose tool never ran", the most common proxy failure); `proxied` + `error_source` enum (above).

### Decision 2: Absence, not `null`, marks "the tool never ran"

`proxy_exit_code` is a `*int` with `omitempty`, so the key is absent when the tool did not run. It must not be emitted as JSON `null`: ClickHouse's `JSONHas` reports a key holding `null` as present, so the pipe's `tool_ran` column would read true and `JSONExtractInt` would read `0` — "the tool succeeded" — for exactly the case the field exists to distinguish.

`proxied` and `cancelled` are emitted unconditionally, `false` included. That is what makes `JSONHas(parameters, 'proxied')` a per-row cutover marker (Decision 7); an `omitempty` bool would leave a post-cutover `false` indistinguishable from a pre-cutover row.

### Decision 3: The origin is declared at the exec site, never inferred

`proc.MarkUserToolExit(err)` wraps a non-zero exit from a tool the user asked for; `proc.IsUserToolExit(err)` reports it. The wrapper is transparent — `Error` and `Unwrap` delegate — so `cmd.ExitCode`'s `errors.As` still reaches the underlying `*exec.ExitError`. This is PR #499's mechanism, kept as-is.

Five call sites mark: `internal/awscli.Exec`, `internal/azurecli.Exec`, and `internal/iac/{cdk,sam,terraform}/cli`. Running through `proc.Run` is deliberately *not* the claim — lstk's own captured-output execs use the same package. Review audited the full non-test exec inventory and confirmed these are exactly the user-requested third-party tools, and that the marker survives every wrapper between the exec site and the emit (`output.NewSilentError`, `output.ExitCodeError`, the tracing wrapper).

**Extensions are not marked and not proxied.** An extension is lstk's own code shipped separately — the mechanism for proprietary lstk commands — so its exit is lstk's, exactly like a built-in's. lstk cannot see whether an extension in turn wraps a third-party CLI; an extension that does attributes that in its own telemetry, joined to the `ext:<name>` event on the conveyed `sessionId`. Should a declared-proxy extension ever be needed, the place for it is the extension contract (`LSTK_EXT_CONTEXT` has no manifest today), not a heuristic in lstk.

**Failure modes are asymmetric on purpose**: forgetting the marker on a future proxy attributes the user's failure to lstk, which shows up as a spike in an lstk-owned metric and gets investigated. Marking one of lstk's own execs hides an lstk bug under the user's name, where nobody looks.

`azurecli.Exec` served both the `lstk az` passthrough and lstk's own `setup azure`/interception calls; #499 rewired `azurecli.Run` onto a shared unexported body so only `Exec` marks. Kept.

**How "the tool ran" is observed.** The exec sites mark failures only; a tool that exited 0 returns plain `nil`. At the emit site the tool is therefore known to have run when the invocation is proxied and *either* the error is marked (`proxy_exit_code` = the marked code) *or* there is no error (`proxy_exit_code` = 0). The second clause is an inference: it holds because every proxy's `RunE` returns `nil` only by way of the exec site (audited: `aws`, `az`, `cdk`, `sam`, `terraform`). The one exception is terraform's developer-only `LSTK_TF_DRY_RUN`, which writes the override file and returns `nil` without running terraform; it is a debugging knob, not a user path, and is accepted. A future proxy that returns `nil` without running its tool would record a phantom `proxy_exit_code: 0`; task 2.1's pinned set is where a reviewer meets that question.

**Known limits**: a tool that fails to *start* (not on PATH, ENOEXEC, permission denied) produces no `*exec.ExitError`, so it records no `proxy_exit_code` and the pipe classifies it `lstk`. That is the intended reading — no wrapped tool produced the failure — and it is one instance of the broader "lstk is not a reliability metric" risk below. A tool that exits 0 and is followed by an lstk failure is recorded the same way (Decision 1).

### Decision 4: `proxied` comes from an explicit annotation, not `DisableFlagParsing`

A `proxyCommandAnnotation` on `aws`, `az`, `cdk`, `sam`, `terraform`. `instrumentCommands` reads it for both `proxied` and the existing `subcommand` derivation. Extensions are resolved rather than registered and cannot carry it; `dispatchExtension` emits `proxied: false` (Decision 3). This is what makes the split reliable: the five proxies are the only commands in the tree with the annotation (pinned by a unit test), and the extension path has no way to acquire it.

**Rationale**: `DisableFlagParsing` is the mechanism that lets a proxy forward unknown flags, not a statement about who the invocation is for; a future command setting it for an unrelated reason would silently start reporting as proxied. The repo already expresses command-level facts this way (`canonicalCommandAnnotation`, `jsonSupportedAnnotation`). One declaration governing both fields keeps them from drifting apart.

The switch is behavior-neutral: exactly five commands set `DisableFlagParsing`; Cobra's own `__complete` sets it too but uses `Run`, not `RunE`, so `walkCommandsWithRunE` never wrapped it; `Annotations` is per-command with no inheritance; aliases share the same `*cobra.Command`, so `tf` is annotated by construction.

**Naming**: the annotation is `proxyCommandAnnotation` and the field is `proxied` — different words, because they describe different things. The annotation marks a *command*; the field describes an *invocation*. `lstk az start-interception` is the case that proves they must not share a name: it runs under a proxy command and is not a proxied invocation.

**Command-name facts a pipe author needs** (verified against the real tree):

| Recorded `command` | Proxied? | Note |
|---|---|---|
| `aws`, `az`, `cdk`, `sam`, `terraform` | yes | `tf` never appears; `commandDisplayName` resolves via `CommandPath()` |
| `az start-interception`, `az stop-interception` | no | own `RunE`; `IN ('az')` excludes them |
| `setup azure` | no | carries the alias `az`, so the retroactive rule must use `IN`, never `LIKE 'az%'` |
| `ext:<name>` | no | lstk's own code; `dispatchExtension` is the sole emitter and always prefixes |

### Decision 5: `cancelled` is lstk's own signal context, not the child's exit code

`cancelled` is true when, at emit time, the root context is done (`ctx.Err() != nil`), the returned error `errors.Is` `context.Canceled`, or `proc.WasInterrupted` reports that the interactive PTY pump forwarded a Ctrl-C. The root context is `main.go`'s `signal.NotifyContext(os.Interrupt, SIGTERM)`, already passed through `root.ExecuteContext` and available as `c.Context()` at the emit site; the `errors.Is` clause catches the TUI's `q`, which sets `context.Canceled` without a signal.

**The interactive PTY path needs the third clause.** When stdin, stdout and stderr are all terminals, `lstk aws`/`lstk az` run the tool in a PTY with the user's terminal in raw mode and pump keystrokes into it (DEVX-1049). Raw mode stops the terminal from turning Ctrl-C into SIGINT; the byte is pumped through and the PTY's line discipline delivers SIGINT to the child alone. lstk's own context never fires. Manual testing found exactly this: Ctrl-C on an interactive `lstk aws` recorded `proxy_exit_code: 130, cancelled: false`, i.e. a proxy failure. `RunInPTY` therefore watches the pumped bytes for ETX and marks the returned error (`proc.WasInterrupted`); the non-interactive paths, where lstk is in the foreground process group and receives the signal itself, are covered by the context clause.

**Why not the child's exit code.** `proc.Run` exists precisely so a wrapped tool *handles* SIGINT and cleans up rather than being SIGKILLed. A tool that cleans up exits with an ordinary code: terraform exits 1, the aws CLI exits 130, the reference extension exits 41. Only an untrapped child yields `signal: interrupt` and `ExitCode() == -1`. And on Windows `ExitCode()` never returns -1 — a Ctrl-C'd child surfaces `STATUS_CONTROL_C_EXIT` (3221225786). lstk's own context is true whether or not the child trapped the signal, and true on Windows, where Go delivers `os.Interrupt` for console Ctrl-C.

**It is emitted raw, with one asymmetry.** An invocation that returned `nil` with a done context records `cancelled: true, exit_code: 0`, but a forwarded Ctrl-C after which the child still exits 0 (Ctrl-C into a pager, then `q`) records `cancelled: false`: `proc.WasInterrupted` rides the returned error, and there is none. The PTY pump also cannot tell a Ctrl-C the child's line discipline turned into SIGINT from one a child in raw mode consumed as a keystroke, so a raw-mode session (an interactive shell, `aws ssm start-session`) that is later exited non-zero after a Ctrl-C reads as cancelled. Both are rare and harmless to the pipe, which reads `exit_code = 0` first. The pipe's `error_source` puts `exit_code = 0 → none` first, so a success is never counted as a cancellation there; the raw field keeps the observation ("a signal arrived") intact for anyone who wants it. `cancelled` must not be read as "this was a failure" on its own.

**Why the emit still happens**: `NotifyContext` replaces the default disposition, so `RunE` returns normally; `Emit` stores `context.WithoutCancel(ctx)` so a cancelled context does not abort the POST; and `Close` hands off to a detached subprocess.

**Accepted**: in the streaming TUIs, `q` sets the same `context.Canceled` as Ctrl-C (`internal/ui/app.go`), so quitting `lstk start` or `lstk logs --follow` with `q` records `cancelled: true` with exit code 1 (verified manually). That is today's exit-code behavior, not something this change introduces — but it means the value counts intentional quits, not only interruptions. `lstk logs` without `--follow` exits 0 on its own and is unaffected.

**Not covered**: SIGHUP is absent from the notify set, so a closed terminal kills lstk at the default disposition and emits nothing at all. Adding it is a one-line change with a real behavior consequence, offered as an optional task rather than folded in.

### Decision 6: `exit_code` is unchanged, and the spec describes what it actually does

`cmd.ExitCode` returns the first `*exec.ExitError` in the chain, then the `--json` convention, then 1.

An earlier draft claimed lstk's own failures "collapse to 1". **That is false today**: `sam`/`cdk` version probes, terraform's S3 backend provisioning, and `lstk update`'s `brew` call all propagate a child's `*exec.ExitError` through `%w` to the top. So some lstk-attributed rows carry brew's or sam's exit code.

**Decision: do not change it.** Aligning them means either changing the process exit code users' scripts observe, or making the recorded `exit_code` diverge from the real one — breaking the invariant `ExitCode`'s doc comment states and #396 established. Neither is worth it, because `proxy_exit_code` removes the harm: the proxy panel groups by `proxy_exit_code`, which an lstk-owned subprocess never populates.

Signal termination of an untrapped child is `-1` in both columns on Unix (surfacing as 255 through `os.Exit(-1)`), and a large positive code on Windows. Such a row with `cancelled = 0` (an external `kill -9`, an OOM kill) classifies `proxy` under the contract below and shows up as its own bucket in the proxied-exits panel; it is rare and left visible rather than special-cased.

### Decision 7: Cutover is marked per row, not by version

A row is post-cutover iff `JSONHas(parameters, 'proxied')`. Because the field is emitted unconditionally (Decision 2) and the raw JSON is kept, this is exact per row and needs no `lstk_version` comparison — which matters because `lstk_version` is a string that includes `0.0.1` and `dev…` builds, and ClickHouse has no semver comparison built in.

Pre-cutover rows derive both facts:

```sql
proxied      := command IN ('aws','az','cdk','sam','terraform')
error_source := exit_code = 0                                         → 'none'
                error_bucket = 'user cancelled'                       → 'cancelled'
                proxied AND startsWith(error_msg, 'exit status ')     → 'proxy'
                otherwise                                             → 'lstk'
```

`ext:` rows are lstk's own (Decision 3) in both eras; the earlier `proxy_error` revision counted them as proxied, and any rule copied from it must not.

**Honest strength of the rule**: `error_msg LIKE 'exit status %'` holds against the tree at this change's commit as convention, not invariant — three lstk-owned subprocess errors reach the top nearly bare and are saved only by a wrapper one frame up (`update failed: %w` over `homebrew.go`'s raw `cmd.Run()`; `azurecli.Run` returning bare when stderr is empty; terraform's `provision.go` runner). Trapped-signal interruptions of wrapped tools cannot be recovered retroactively at all.

### Decision 8: The error code rides the returned error, via `output.Fail`

An `ErrorEvent.Code` used to live only on the event the sink consumed; `PlainSink` and `TUISink` discard it, and the error value `instrumentCommands` receives was a bare `SilentError`. Manual testing showed the cost: `lstk aws --account 123 s3 ls`, a missing `aws` binary and a wrong-type endpoint all classify as lstk's failure with nothing to tell them from a defect.

`SilentError` now carries `Code`, set by `output.Fail(sink, event, err)`, which emits and returns in one call. `commandResult` reads it with `output.ErrorCodeOf` and emits `error_code` plus its `Category()`. The category is emitted from Go rather than derived in the pipe because the code-to-category map is versioned with the binary.

**Coverage is the limit, and it is measurable.** Only sites that go through `Fail` with a code populate the fields; a site that emits an event and returns a bare error, or returns a bare error to Cobra's fallback printer, records nothing. Absence therefore means "unclassified", which the pipe should report as a share so regressions are visible. Nothing static forces a site to classify; the closest guard is the `--json` envelope, which renders the same code and falls back to `INTERNAL_ERROR`.

**Not emitted**: `CANCELLED` (the `cancelled` field is the observation; a code would be the same fact twice) and anything for proxied tool exits (no `ErrorEvent` is shown for them; `proxy_exit_code` is their axis).

## Analytics contract

Changes to `fct_lstk_command.pipe` (repo `localstack/localstack-dwh`). Field names below are proposals for the pipe author.

```sql
-- unpacked: read only
toUInt8(JSONHas(parameters, 'proxied'))            AS has_origin,       -- post-cutover row
toUInt8(JSONExtractBool(parameters, 'proxied'))    AS proxied_raw,
toUInt8(JSONHas(result, 'proxy_exit_code'))        AS tool_ran,         -- never read proxy_exit_code without it
toInt32(JSONExtractInt(result, 'proxy_exit_code')) AS proxy_exit_code,
toUInt8(JSONExtractBool(result, 'cancelled'))      AS cancelled,
JSONExtractString(result, 'error_code')            AS error_code,        -- '' = unclassified
JSONExtractString(result, 'error_category')        AS error_category,

-- classified: the rule, once, over both eras
if(has_origin = 1, proxied_raw, command IN ('aws','az','cdk','sam','terraform')) AS proxied,
multiIf(
  exit_code = 0,                                                        'none',
  has_origin = 1 AND cancelled = 1,                                     'cancelled',
  has_origin = 0 AND error_bucket = 'user cancelled',                   'cancelled',
  has_origin = 1 AND tool_ran = 1 AND proxy_exit_code != 0,             'proxy',
  has_origin = 0 AND proxied = 1 AND startsWith(error_msg, 'exit status '), 'proxy',
                                                                        'lstk') AS error_source
```

`has_result = 1` remains the guard on every rate (it already is on both panels).

```sql
-- Headline: failure rate of lstk's own commands. Does not move with `lstk aws`
-- adoption, because proxy invocations are out of BOTH terms.
countIf(error_source = 'lstk' AND proxied = 0) / countIf(has_result = 1 AND proxied = 0)

-- Companion: lstk-attributable failures across all invocations, preflight
-- failures on proxy commands included. Moves with proxy adoption through the
-- denominator — a volume-weighted view, not a reliability metric.
countIf(error_source = 'lstk') / countIf(has_result = 1)

-- Product-health: how often users' wrapped-tool calls fail.
countIf(error_source = 'proxy') / countIf(has_result = 1 AND proxied = 1)

-- Reliability (the metric DEVX-1004 could not build before): lstk failures
-- that are not the user's input or environment. Which categories count is
-- a team decision; USAGE, CONFIG, AUTH and RUNTIME are clearly not lstk's.
countIf(error_source = 'lstk' AND error_category NOT IN ('USAGE','CONFIG','AUTH','RUNTIME'))
  / countIf(has_result = 1 AND proxied = 0)

-- Coverage: the share of lstk failures with no classification. Watch it.
countIf(error_source = 'lstk' AND error_code = '') / countIf(error_source = 'lstk')

-- Panel 15 "Top command errors": replace
--   exit_code != 0 AND error_bucket != 'user cancelled'
-- with
--   error_source = 'lstk'

-- New panel: top proxied tool exits. `command` is required, not optional —
-- `cdk deploy` and `sam deploy` would merge, and codes from different tools
-- are not comparable.
WHERE error_source = 'proxy' GROUP BY command, subcommand, proxy_exit_code
```

**The denominator is not "every invocation".** Two populations distort it, neither introduced nor fixed here:

- Commands that cannot meaningfully fail inflate it. `lstk completion bash` has a `RunE` and is instrumented, and the documented setup is `eval "$(lstk completion bash)"` in a shell rc — one guaranteed-success event per new terminal. `version`, `docs`, and `config path` are in the same class. Exclude them explicitly.
- Invocations that fail before `RunE` emit nothing. `instrumentCommands` wraps `RunE` only, and Cobra skips `RunE` when `PreRunE` errors — so a malformed `config.toml`, `--config /nonexistent`, a flag-parse error, and an unresolved extension name produce **zero** events. lstk-owned failures missing from both terms.

**Segment before reading.** Every event carries `caller_type`, `is_ci`, `machine_id`, and `session_id`. Both panels already segment by `caller_type` and audience; keep that, and exclude `is_ci` from anything human-facing.

## Risks

- **`error_source = 'lstk'` is not a measure of lstk's reliability.** For `lstk aws` it includes: the AWS CLI not on PATH, `--account` misplaced, an invalid account id, a bad `--endpoint-url`, a non-AWS endpoint, Docker down, the emulator not running. None is an lstk defect. The field answers "did lstk fail to complete the invocation", which is what the panel should be titled.
- **The why axis is only as good as its coverage.** Decision 8 makes `error_code` reachable, but at this change's commit only the converted sites populate it; 67 `ErrorEvent` emit sites exist and most set no code. Until the remainder are classified, the reliability metric's numerator over-counts (unclassified failures cannot be excluded) and the coverage query above is the honest companion.
- **The "tool ran" inference on success** (Decision 3) is a convention over the proxies' `RunE` bodies, pinned by a test on the annotated set rather than by the type system.
- **Three fields land before any panel reads them.** Events are additive and old consumers ignore unknown keys, so the CLI side can ship first; the ranking stays wrong until the pipe and panels change.
