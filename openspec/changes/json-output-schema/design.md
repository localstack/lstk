## Context

`lstk` is a Cobra CLI with ~25 built-in commands plus five proxy commands (`aws`, `terraform`, `cdk`, `sam`, `az`) that forward verbatim to a wrapped tool, and a Git-style extension mechanism for anything else. The `add-json-flag` change (already merged) added a global `--json` flag, threaded it through `internal/env.Env`, and made it force non-interactive rendering — but deliberately implemented no actual JSON output. Every built-in command currently rejects `--json` via a `jsonSupportedAnnotation` opt-in gate that nothing has opted into yet.

Output today flows through `internal/output.Sink`: domain code (`internal/container`, `internal/snapshot`, `internal/volume`, etc.) emits one of ~20 `Event` types (`MessageEvent`, `ErrorEvent`, `InstanceInfoEvent`, `TableEvent`, `SnapshotShownEvent`, ...) and `cmd/` selects a `Sink` implementation at the command boundary — `NewPlainSink` for non-interactive text, `NewTUISink` for the Bubble Tea interface. This design adds a third sink, `EnvelopeSink`, so the existing domain code needs no changes to support JSON — only the small set of call sites whose current event fields don't carry enough machine-readable information (mostly free-text `ErrorEvent.Title`) need an added classification.

Errors are not currently enumerated anywhere: they're either an `ErrorEvent` with human prose, or a bare `error` returned up to `cmd/root.go`'s `Execute()`, which prints `Error: %v` to stderr. There is no existing error-code concept to build on.

## Prior Art

Before finalizing the envelope, the following were reviewed for how comparable tools structure JSON output and errors:

| Tool | JSON shape | Error handling |
|---|---|---|
| [Kubernetes API](https://kubernetes.io/docs/reference/kubernetes-api/common-definitions/status/) | Single object per resource; a dedicated `Status` kind for errors | `status: Failure`, an enumerated `reason` (`NotFound`, `Conflict`, `Invalid`, `Timeout`, `Unauthorized`, `Forbidden`, `InternalError`, ...), a human `message`, and a suggested HTTP-style `code` |
| [Terraform `plan`/`apply`/`refresh`/`test -json`](https://developer.hashicorp.com/terraform/internals/machine-readable-ui) | NDJSON — one object per line, with `@level`/`@message`/`@module`/`@timestamp`/`type` on every line; the first line always declares the wire format version | Errors/warnings arrive as interleaved `diagnostic`-typed lines, not a single terminal error — the stream itself is the result |
| [GitHub CLI (`gh`)](https://cli.github.com/manual/gh_help_exit-codes) | `--json field1,field2` — caller supplies an explicit fieldset allowlist | Exit codes are **not** just ok/error: `0` ok, `1` generic failure, `2` cancelled, `4` auth required, with individual commands adding more (`gh pr checks` uses `8` for "pending") |
| [Docker CLI](https://github.com/moby/moby/issues/46906) | Go-template `--format '{{json .}}'`, one object per resource | No structured error convention at all (plain text to stderr); the JSON side has shipped real bugs — missing array brackets, inconsistent NDJSON-vs-array across platforms, stdout/stderr interleaving in Compose |
| AWS CLI v2 (2.34.0+) | Bare API response shape, no added envelope | `--cli-error-format json` → `{"Code", "Message", "Type"}` where `Type` is `Sender` (caller's fault) vs `Service` (their fault) — a retry-relevant classification orthogonal to the specific code |
| [JSON-RPC 2.0](https://www.jsonrpc.org/specification) *(a protocol, not a tool)* | `{"result": ..., "error": ...}`, mutually exclusive, both keys always present | `error: {code, message, data}` — negative integers reserved by spec for transport failures, positive range open for application-defined errors |

This validated the core envelope choice (a single discriminated `data`/`error` object, both keys always present, is essentially JSON-RPC 2.0's `result`/`error` split) and Kubernetes' `reason`-enum-plus-message pattern (already reflected in `error.code`/`error.message`). It also surfaced two worthwhile, low-cost additions adopted below — reserved exit codes for the categories a shell script branches on most often (matching `gh`'s convention), and a `retryable` flag orthogonal to the specific code (matching AWS's `Sender`/`Service` split) — plus two alternatives considered and declined for this round: NDJSON progress-streaming for slow-but-bounded commands (Terraform/Pulumi's approach to `apply`) and GitHub's sparse-fieldset selection (see Non-Goals and Open Questions).

None of the five tools reviewed support more than one structured output format with a single shared mechanism in the way lstk intends to (JSON now, YAML later — see the naming decisions below) — the closest precedent is Kubernetes' own tooling, where `kubectl get -o json` and `-o yaml` share one underlying object model and differ only in the final encoding step. That single fact shaped how this change names its new types: see "Decisions" for the accumulation/serialization split and the `json:`-tags-only choice it enables.

## Goals / Non-Goals

**Goals:**
- One JSON envelope shape used by every JSON-capable command, so a script or IDE integration learns the contract once.
- Machine-readable errors: a fixed, documented `error.code` enum, never a string a script would have to pattern-match.
- No silent gaps: a JSON-capable command must never fall through to a bare stderr line — every failure, including ones we didn't anticipate, renders as the envelope.
- Cover the full built-in command surface in one pass, so the schema is validated against every real shape it needs to hold, not just the easy cases.

**Non-Goals:**
- Proxy commands (`aws`, `terraform`, `cdk`, `sam`, `az` passthrough) and extension dispatch. Both already have a settled, separate contract (forward untouched / own output respectively) per the `json-flag` spec; this proposal doesn't reopen that.
- `login` and `config profile`: both require an interactive terminal unconditionally today (browser OAuth, TTY prompts) and gain no JSON path here. A future change could add a non-interactive device-code-style login; out of scope now.
- Actually implementing JSON rendering for all ~25 commands in one PR. This proposal defines the schema and the infrastructure; tasks.md sequences the command-by-command rollout so it can land incrementally behind the same per-command opt-in the flag already uses.
- Changing plain-text/TUI output in any way. `--json` is strictly additive.
- **Considered and declined: NDJSON progress-streaming for slow-but-bounded commands.** Terraform's `apply -json` and Pulumi's engine-event stream emit live progress lines (pull/apply/provision progress) before a final result, rather than staying silent until done. `start` (image pulls) and `snapshot load`/`save` (large state transfers) are exactly this kind of slow-but-bounded operation, and today's `EnvelopeSink` design (Decision above) silently drops that progress instead of streaming it. This is deliberately deferred rather than folded in here — see Open Questions — because it changes the wire shape for those specific commands (NDJSON-with-a-final-result-line, distinct from both the single-envelope and the pure-stream `logs --follow` shapes already defined) and deserves its own decision rather than being absorbed into this pass.
- **Considered and declined: sparse fieldset selection (`gh`-style `--json field1,field2`).** Letting a caller pick which fields of `data` to receive keeps payloads small and makes adding fields always backward-compatible. Declined for v1: lstk's payloads are small relative to `gh`'s (nothing like a hundred-row PR list), and `schemaVersion` already provides an evolution path, so the added flag-parsing surface isn't worth it yet. Revisit if a command's `data` shape grows large enough that most callers only want a slice of it.
- **`-v`/`--version` gaining JSON output.** A permanent gap, not a deferred one — see the "Decision: `-v`/`--version` stays JSON-incapable" below for why.
- **How a future second output format is selected on the command line** (e.g. whether `--json` itself changes shape). Explicitly deferred — not addressed by this change at all, beyond noting that the new type/capability names here (`EnvelopeSink`, `EnvelopeError`, `EnvelopeAction`, `output-envelope`, `error-codes`) don't depend on the answer.

## Decisions

### Decision: A dedicated `EnvelopeSink` accumulates events; serialization is a separate, swappable step

**Alternative considered**: have each command build its own result struct and marshal it directly, bypassing the sink/event system for the JSON path.

Rejected because it would require every domain function to grow a second, format-specific return value or callback alongside the events it already emits — doubling the surface area CLAUDE.md's "Output Routing and Events" section asks every feature to go through, and re-introducing exactly the kind of per-command bespoke logic this proposal exists to avoid. Instead, `output.EnvelopeSink` implements `Sink` like `PlainSink`/`TUISink` do: it type-switches on each emitted event, accumulates the ones that carry the command's actual result (`InstanceInfoEvent`, `TableEvent`, `ResourceSummaryEvent`, `SnapshotShownEvent`, `PodSnapshotSavedEvent`, etc.) into the envelope's `data`, routes `ErrorEvent` into `error`, and silently drops purely presentational events (`SpinnerEvent`, `ContainerStatusEvent`, `ProgressEvent`, `DeferredEvent`) that have no place in a single terminal result. `MessageEvent` with `SeverityWarning` is kept, not dropped — it becomes an entry in the envelope's `warnings` array (see below), since "an update is available" or "DNS fallback used" is information a script may care about even on success.

This accumulation step has nothing JSON-specific about it — it would be identical if the output were YAML or anything else structured. Only the final step differs: `EnvelopeSink` is constructed with an `output.Format` (`output.FormatJSON` is the only value that exists today), and `(*EnvelopeSink).Result() Envelope` returns the accumulated struct for the command boundary to marshal according to that format and write once to stdout. The name is chosen for the mechanism (accumulating events into an envelope), not the one format it currently serializes to — a deliberate choice given YAML output using the same envelope is a known future direction (not being built now, but not something this naming should have to be undone for later). This mirrors `FormatEventLine`'s single-dispatch-point pattern already used for plain-text rendering, just producing a struct instead of a string.

### Decision: `ErrorEvent` gains a `Code ErrorCode` field instead of a parallel format-specific error event

**Alternative considered**: a new `EnvelopeErrorEvent` type, separate from `ErrorEvent`, emitted only on the structured-output path.

Rejected for the same reason as above — it would mean every error call site needs two emissions (or an if/else on output mode inside domain code, which CLAUDE.md's routing rules explicitly forbid). Instead `ErrorEvent` gets one additive field:

```go
type ErrorEvent struct {
    Title   string
    Summary string
    Detail  string
    Actions []ErrorAction
    Code    ErrorCode // new; empty means "not yet classified" — EnvelopeSink falls back to INTERNAL_ERROR
}
```

`PlainSink`/`TUISink` ignore the new field entirely (matching how they already ignore fields they don't render), so this is fully backward-compatible. Call sites inside a JSON-capable command's reachable code path are updated to set `Code`; sites not yet reached by any JSON-capable command are left as-is and simply render as `INTERNAL_ERROR` if a future command opts in without also classifying them — a visible, honest fallback rather than an invented code.

### Decision: The envelope is one flat struct, not per-command discriminated types

```go
type Envelope struct {
    SchemaVersion int            `json:"schemaVersion"`
    Command       string         `json:"command"`
    Status        string         `json:"status"` // "ok" | "error"
    Data          any            `json:"data"`
    Warnings      []Warning      `json:"warnings"`
    Error         *EnvelopeError `json:"error"`
}

type Warning struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

type EnvelopeError struct {
    Code      ErrorCode        `json:"code"`
    Message   string           `json:"message"`
    Retryable bool             `json:"retryable"`
    Details   map[string]any   `json:"details,omitempty"`
    Actions   []EnvelopeAction `json:"actions,omitempty"`
}

type EnvelopeAction struct {
    ID      string `json:"id"`      // e.g. "start-emulator"
    Command string `json:"command"` // e.g. "lstk"
}
```

`data` is `null` when `status` is `"error"`, and `error` is `null` when `status` is `"ok"` — always both keys present so a script can do `resp.error` / `resp.data` without existence checks. `warnings` is always an array (possibly empty), never `null`, for the same reason.

**Alternative considered**: version each command's payload independently (`"schemaVersion": {"envelope": 1, "status": 2}`). Rejected as premature — all commands ship from the same `lstk` binary version, so a single envelope `schemaVersion`, bumped only on a breaking change to the envelope itself or to any one command's `data` shape, is sufficient and far simpler for a script to check (`if resp.schemaVersion !== 1: bail`).

`EnvelopeAction` deliberately mirrors `output.ErrorAction` (`Label`/`Value`) but renamed for a machine reader: `id` is a stable slug a script can switch on, `command` is the literal shell command a human or script could run (e.g. `{"id": "start-emulator", "command": "lstk"}` where plain text today shows `==> Start LocalStack: lstk`).

**Naming note**: `Envelope`, `Warning`, `EnvelopeError`, `EnvelopeAction`, and `ErrorCode` all avoid a `JSON`-specific name on purpose, matching `EnvelopeSink` above — none of these types describe anything about JSON specifically, only about the envelope's shape. `ErrorCode` values themselves (`EMULATOR_NOT_RUNNING`, etc.) are just strings and were already format-neutral without any renaming needed.

### Decision: Struct tags stay `json:`-only — no parallel `yaml:` tags to maintain later

Every field above defines its wire name once, via a `json:` tag. The obvious worry: if YAML output is added later, won't it need its own `yaml:` tags, maintained in parallel and prone to drifting out of sync (e.g. `schemaVersion` in JSON but a plain `yaml.Marshal` defaulting to `schemaversion` in YAML, since `gopkg.in/yaml.v3` — already an indirect dependency in `go.mod` today — doesn't read `json:` tags at all)?

It doesn't need to, based on precedent: [`sigs.k8s.io/yaml`](https://github.com/kubernetes-sigs/yaml) — the library `kubectl` itself uses so `-o json` and `-o yaml` produce identical field names from Kubernetes API structs that only carry `json:` tags — works by marshaling to JSON first via `encoding/json`, then converting those bytes to YAML, rather than walking the struct with a YAML-native encoder. A future YAML format for lstk's envelope can use the same trick: `json.Marshal(envelope)` then a JSON→YAML byte conversion, so `Envelope`/`EnvelopeError`/etc. never need a second set of tags, and the two output formats are guaranteed to agree on every field name by construction, not by discipline. Nothing to implement now — just a reason the current `json:`-only tagging isn't a trap.

**Alternative considered**: add `yaml:` tags alongside `json:` tags now, "for free," since the fields already exist. Declined — it isn't actually free (every future field addition would need to remember both tags, and a human easily introduces a typo'd mismatch between them that a JSON-first-then-convert approach makes structurally impossible), and there's no consumer of the `yaml:` tag yet to catch such a mismatch in review.

### Decision: Exit codes stay coarse, with two reservations for the categories scripts branch on most

`0` on success, `1` when the envelope's `status` is `"error"` for any code not covered below, `2` only for a Cobra-level usage error that couldn't be attributed to any command logic. Scripts that need finer branching read `error.code` from the JSON, not the exit code — POSIX exit codes are a poor fit for a 28-entry enum, and every code already round-trips through the same envelope on stdout.

Two exceptions, following `gh`'s precedent of reserving specific exit codes for the failures a shell script is most likely to want to branch on *without* parsing stdout first: exit `3` when `error.code` is `CONFIRMATION_REQUIRED` (a script can retry the exact same invocation with `--force` appended), and exit `4` when `error.code` is `AUTH_REQUIRED` (a script can shell out to `lstk login` and retry), mirroring `gh`'s own `4` for "needs auth". These two are picked because they came up across nearly every command in the Command Catalog and have an obvious, mechanical remediation — everything else stays behind `error.code` on exit `1`, since inventing an exit code per category doesn't scale to 28 codes and most failures don't have a one-line automated fix anyway.

| Exit code | Meaning |
|---|---|
| `0` | `status: "ok"` |
| `1` | `status: "error"`, any code other than the two below |
| `2` | Unattributed Cobra usage error (see below) |
| `3` | `error.code == "CONFIRMATION_REQUIRED"` |
| `4` | `error.code == "AUTH_REQUIRED"` |

### Decision: Error objects carry a `retryable` flag, orthogonal to `code`

`error.code` answers "what went wrong"; it doesn't answer "is it worth trying again." `RUNTIME_UNAVAILABLE` and `NETWORK_ERROR` are usually transient — a polling script should back off and retry. `VALIDATION_ERROR` and `CONFIRMATION_REQUIRED` never resolve themselves — retrying the identical invocation just fails the same way again. Rather than making a script maintain its own hardcoded list of "which of these 28 codes are worth retrying," `retryable` is a boolean on every error object, mirroring AWS CLI's `Sender`/`Service` split (their two-value version of the same idea). It is a **static property of the code**, not computed per-instance — the same code always carries the same `retryable` value, documented alongside the code table in the `error-codes` capability (e.g. `RUNTIME_UNAVAILABLE: true`, `VALIDATION_ERROR: false`).

**Alternative considered**: reuse AWS's exact `Sender`/`Service` two-value classification instead of a boolean. Declined — lstk has no analogous client/server split (everything runs locally), and "is it worth automatically retrying" is the one question a script actually needs answered; a boolean says that directly without requiring the caller to know that, say, `Sender` means "don't retry" in lstk's context too.

### Decision: `RUNTIME_UNAVAILABLE`, not `DOCKER_UNAVAILABLE` — the code names the abstraction, not the concrete implementation

An earlier draft of this proposal used `DOCKER_UNAVAILABLE`, following the concrete type actually implemented today (`runtime.NewDockerRuntime`, used at every call site in `cmd/`). That's the wrong level to name a stable, machine-readable contract at: `internal/runtime.Runtime` is already an interface (`IsHealthy`, `EmitUnhealthyError`, etc.) with exactly one implementation, and CLAUDE.md's own architecture section describes it as "Abstraction for container runtimes (Docker, Kubernetes, etc.) — currently only Docker implemented." Baking "Docker" into the error taxonomy would mean either a breaking rename the day a second runtime lands (Podman, Rancher Desktop, and Finch are Docker-API-compatible and might work with zero runtime-layer changes; Kubernetes would need a distinct `Runtime` implementation), or shipping a permanently inaccurate code once one did. `RUNTIME_UNAVAILABLE` matches the interface name that already exists in the codebase, not a new abstraction invented for this proposal.

**Alternative considered**: `CONTAINER_RUNTIME_UNAVAILABLE`, matching CLAUDE.md's exact phrase "container runtime." Declined only for brevity — `runtime` is unambiguous in this CLI (there's no other kind of "runtime" lstk deals with), and it matches the Go package name (`internal/runtime`) and interface name (`runtime.Runtime`) directly, which is arguably a more durable anchor than a doc phrase that could itself be reworded later.

### Decision: Usage errors are best-effort JSON

Cobra's own flag/argument validation happens before `RunE` runs, via `PersistentPreRunE`/`Args` checks, and today produces a plain `Error: %v` on stderr with `SilenceErrors`/`SilenceUsage` set (`cmd/root.go:227-239`). A new wrapper — parallel to `requireJSONSupport`'s existing tree-walk — catches an error returned from that pre-run stage and, **if `--json` was already successfully parsed by that point**, renders it as the envelope with `error.code = USAGE_ERROR`. If the malformed flag appears *before* `--json` in the invocation (so Cobra never got far enough to see `--json` at all — e.g. `lstk --jso status`), lstk cannot know JSON was wanted and falls back to today's plain-text usage error, exit `2`. This is a narrow, honestly-documented gap rather than an attempt to solve flag-parsing order in general.

### Decision: `status`'s JSON mode reports every configured emulator, not just the first non-running one

Today, plain-text `status` loops over configured containers and returns immediately (as an `ErrorEvent` + exit 1) on the first one that isn't running (`internal/container/status.go:32-41`), so with AWS configured-but-stopped and Snowflake running, `lstk status` never even checks Snowflake. For JSON, this is the wrong shape for "a script wants to know what's up" — it silently truncates. JSON mode always iterates every configured emulator and reports `running: true/false` for each, with the running ones' full detail and the non-running ones as just `{"type": "...", "running": false}`. This only changes `--json` behavior; plain-text `status` is unchanged.

### Decision: `logs --follow --json` is NDJSON, not one growing envelope

A follow-mode log stream has no natural "done" moment to close a single JSON object around. Under `--json --follow`, each line is its own compact JSON object, newline-delimited (NDJSON), with a `type` field instead of `status` (`"log"` for a line, `"error"` if the stream itself fails):

```json
{"schemaVersion":1,"command":"logs","type":"log","data":{"source":"emulator","level":"info","line":"Ready."}}
```

Bounded `logs --json` (no `-f`) uses the normal single-envelope shape with `data.lines` as an array of the same `{source, level, line}` objects — it terminates naturally, so the standard envelope fits.

### Decision: `-v`/`--version` stays JSON-incapable, permanently — no new subcommand added to work around it

Cobra's built-in version flag (`root.InitDefaultVersionFlag()`) is handled inside `Command.execute()` before `PersistentPreRunE`/`RunE` run at all, so it never passes through `requireJSONSupport` or any JSON dispatch this design adds — there is no hook to intercept it without reaching into Cobra's internals.

**Alternative considered**: add a real `lstk version` subcommand (like `docker version`/`kubectl version`), going through the normal `RunE` path so it can support `--json` (`{"version": "2.3.1"}`), leaving `-v`/`--version` as a plain-text-only flag alongside it. Initially chosen, then reconsidered and dropped: it's new CLI surface invented solely to route around Cobra's short-circuit, for a cosmetic gap (the version is not information any of the surveyed prior art or real usage suggests scripts urgently need in JSON form — unlike, say, `status` or `snapshot show`).

**Alternative considered**: stop using Cobra's `c.Version`/`InitDefaultVersionFlag()` mechanism entirely, and instead register `--version`/`-v` as an ordinary flag checked inside root's own `RunE`, alongside `--persist` and the snapshot flags it already reads there. This would let `--version` flow through the same JSON dispatch as everything else without adding a subcommand. Also rejected: root's `PreRunE` is `initConfigDeferCreate`, which loads (and on first run, creates) the config file. Moving version-handling into `RunE` means it now runs *after* `PreRunE`, so `lstk --version` would start depending on config loading succeeding — a real regression, since today it works even with a missing or malformed config file, matching the convention `git --version`/`docker --version`/`terraform --version` all share (a version check is often run specifically to debug a broken environment, so it shouldn't be able to fail because of one).

**Decision**: accept the gap. `-v`/`--version` never gets `--json` support, documented as a permanent limitation alongside `login` and `config profile` (see Command Catalog and Non-Goals) rather than solved by inventing a command around it.

## Command Catalog

This is the artifact meant for human review: every built-in command, whether it gets `--json` support in this proposal, its `data` shape inside the envelope, and the `error.code`s it can realistically produce. Field names are `camelCase` JSON; durations are seconds (`uptimeSeconds`), timestamps are RFC 3339 strings, byte counts are `sizeBytes`. Example `name` values below (e.g. `"localstack-aws"`) are the real default container name for the AWS emulator — `fmt.Sprintf("localstack-%s", c.Type)` in `internal/config/containers.go`'s `ContainerConfig.Name()`, not a placeholder — since a custom `container_name` isn't currently configurable, this is what every reader will actually see. It names the **container**, not the underlying Docker **image** (`localstack/localstack:latest`, resolved separately by `ContainerConfig.Image()`); the two are easy to conflate but the `name` field here is always the former, matching `InstanceInfoEvent.ContainerName`. Commands and flags not listed (`aws`, `terraform`, `cdk`, `sam`, `az` passthrough, extension dispatch, `docs`, `completion`, `help`, `login`, `config profile`, and the `-v`/`--version` flag) are explicitly out of scope — see Non-Goals.

### Emulator lifecycle

**`lstk start`** — `data`: an emulator entry per configured container, plus whether a configured snapshot was auto-loaded.
```json
{
  "emulators": [
    {"type": "aws", "name": "localstack-aws", "host": "localhost:4566", "version": "3.9.0", "alreadyRunning": false, "persist": false}
  ],
  "snapshotLoaded": null
}
```
Codes: `RUNTIME_UNAVAILABLE`, `AUTH_REQUIRED`, `LICENSE_INVALID`, `LICENSE_UNSUPPORTED_TAG`, `IMAGE_PULL_FAILED`, `EMULATOR_START_FAILED`, `SNAPSHOT_NOT_FOUND` (bad `--snapshot`), `VALIDATION_ERROR` (`--snapshot` with `--no-snapshot`).

**`lstk stop`** — `data`: which configured emulators were actually running and got stopped.
```json
{"emulators": [{"type": "aws", "name": "localstack-aws", "wasRunning": true}]}
```
Codes: `RUNTIME_UNAVAILABLE`.

**`lstk restart`** — `data`: the stop result and the start result, reusing both shapes above.
```json
{"stopped": [{"type": "aws", "name": "localstack-aws", "wasRunning": true}], "started": [{"type": "aws", "name": "localstack-aws", "host": "localhost:4566", "version": "3.9.0", "alreadyRunning": false, "persist": false}]}
```
Codes: `RUNTIME_UNAVAILABLE`, `AUTH_REQUIRED`, `LICENSE_INVALID`, `EMULATOR_START_FAILED`.

**`lstk status`** — `data`: one entry per *configured* emulator (not just running ones — see the status-behavior Decision above); non-running entries omit the detail fields entirely rather than nulling them out.
```json
{
  "emulators": [
    {
      "type": "aws", "running": true, "name": "localstack-aws", "version": "3.9.0",
      "host": "localhost:4566", "uptimeSeconds": 1234, "persistence": false,
      "resourceSummary": {"resources": 12, "services": 4},
      "resources": [{"service": "s3", "name": "my-bucket", "region": "us-east-1", "account": "000000000000"}]
    },
    {"type": "snowflake", "running": false}
  ]
}
```
Codes: `RUNTIME_UNAVAILABLE`.

**`lstk logs`** (bounded) — `data.lines` is the same `{source, level, line}` shape used by the NDJSON stream variant.
```json
{"lines": [{"source": "emulator", "level": "info", "line": "Ready."}]}
```
`lstk logs --follow` (NDJSON, one compact object per line, no enclosing envelope — see Decisions):
```json
{"schemaVersion": 1, "command": "logs", "type": "log", "data": {"source": "emulator", "level": "info", "line": "Ready."}}
```
Codes: `RUNTIME_UNAVAILABLE`, `EMULATOR_NOT_RUNNING`.

**`lstk reset`** — `data`: confirmation of what was reset (AWS-only today).
```json
{"emulator": {"type": "aws", "name": "localstack-aws"}, "reset": true}
```
Codes: `EMULATOR_NOT_CONFIGURED` (no AWS container configured), `EMULATOR_NOT_RUNNING`, `CONFIRMATION_REQUIRED` (no `--force` outside a TTY), `RUNTIME_UNAVAILABLE`.

**`lstk volume path`** — `data`: one path per configured container.
```json
{"volumes": [{"type": "aws", "path": "/Users/x/Library/Caches/lstk/aws"}]}
```
Codes: `CONFIG_INVALID`.

**`lstk volume clear`** — `data`: which volumes were actually cleared.
```json
{"cleared": [{"type": "aws", "path": "/Users/x/Library/Caches/lstk/aws"}]}
```
Codes: `EMULATOR_NOT_CONFIGURED` (bad `--type`), `CONFIRMATION_REQUIRED` (no `--force` outside a TTY).

### Configuration and auth

**`lstk config path`** — `data`: the resolved (or explicitly `--config`-overridden) path.
```json
{"path": "/Users/x/.config/lstk/config.toml"}
```
Codes: `CONFIG_NOT_FOUND` (`--config` path doesn't exist), `CONFIG_INVALID`.

**`lstk logout`** — `data`: whether there was anything to log out of, and any emulators still running with the now-removed token.
```json
{"loggedOut": true, "stillRunning": []}
```
When already logged out, this is still `status: "ok"` with `"loggedOut": false` — logout is idempotent today (`errors.Is(err, auth.ErrNotLoggedIn)` returns `nil`), and JSON mode preserves that rather than inventing a new error for it. Codes: none expected in normal operation; `INTERNAL_ERROR` as the universal fallback.

**`lstk setup aws`** — `data`: the profile that was written and whether the LocalStack hostname resolved.
```json
{"profile": "localstack", "written": true, "dnsOk": true}
```
Codes: `CONFIRMATION_REQUIRED` (existing profile differs, no `--force`).

**`lstk setup azure`** — `data`: the isolated config dir and the cloud that was registered.
```json
{"configDir": "/Users/x/.config/lstk/azure", "cloudRegistered": "LocalStack"}
```
Codes: `DEPENDENCY_MISSING` (`az` CLI not on `PATH`), `RUNTIME_UNAVAILABLE`, `EMULATOR_NOT_RUNNING`, `DNS_RESOLUTION_REQUIRED`.

**`lstk az start-interception`** — `data`: same shape as `setup azure`, plus the resolved endpoint.
```json
{"cloudRegistered": "LocalStack", "endpoint": "https://azure.localhost.localstack.cloud:4566"}
```
Codes: `DEPENDENCY_MISSING`, `RUNTIME_UNAVAILABLE`, `EMULATOR_NOT_RUNNING`, `DNS_RESOLUTION_REQUIRED`.

**`lstk az stop-interception`** — `data`: what changed, or confirmation nothing did (LocalStack wasn't the active cloud).
```json
{"switchedFrom": "LocalStack", "switchedTo": "AzureCloud", "changed": true}
```
Codes: `VALIDATION_ERROR` (`--cloud` not a registered cloud).

### Snapshots

**`lstk snapshot save`** — `data`: one shape covering local/pod/S3 destinations, discriminated by `kind`.
```json
{"kind": "pod", "location": "pod:my-baseline", "podName": "my-baseline", "version": 4, "services": ["s3", "lambda"], "sizeBytes": 245678}
```
(`kind` is `"local"` | `"pod"` | `"s3"`; `podName`/`version` are `null` for `kind: "local"`.) Codes: `RUNTIME_UNAVAILABLE`, `EMULATOR_NOT_RUNNING`, `AUTH_REQUIRED` (pod), `CREDENTIALS_MISSING` (S3, no AWS creds resolvable), `SNAPSHOT_BUCKET_NOT_FOUND`, `SNAPSHOT_REMOTE_ERROR`, `VALIDATION_ERROR` (bad pod name).

**`lstk snapshot load`** — `data`: what was loaded and whether it started the emulator to do so.
```json
{"source": "pod:my-baseline", "services": ["s3", "lambda"], "emulatorStarted": false, "mergeStrategy": "account-region-merge"}
```
Codes: `SNAPSHOT_NOT_FOUND`, `SNAPSHOT_INVALID_REF`, `SNAPSHOT_REMOTE_ERROR`, `SNAPSHOT_BUCKET_NOT_FOUND`, `CREDENTIALS_MISSING`, `AUTH_REQUIRED`, `RUNTIME_UNAVAILABLE`, `EMULATOR_START_FAILED`, `VALIDATION_ERROR` (bad `--merge`).

**`lstk snapshot list`** — `data`: the platform or S3 location queried, and the snapshots found.
```json
{"location": "platform", "snapshots": [{"name": "my-baseline", "version": 4, "lastChanged": "2026-07-01T12:00:00Z"}]}
```
Codes: `AUTH_REQUIRED` (platform), `CREDENTIALS_MISSING`/`SNAPSHOT_BUCKET_NOT_FOUND` (S3), `SNAPSHOT_REMOTE_ERROR`, `EMULATOR_NOT_RUNNING` (S3 list requires a running emulator).

**`lstk snapshot show`** — `data`: mirrors `SnapshotShownEvent` directly (the richest existing struct, see design Context).
```json
{
  "name": "my-baseline", "version": 4, "created": "2026-06-01T00:00:00Z", "sizeBytes": 245678,
  "localstackVersion": "3.9.0", "message": "", "services": ["s3"],
  "resources": [{"service": "s3", "counts": [{"noun": "buckets", "count": 3}]}]
}
```
Codes: `AUTH_REQUIRED`, `SNAPSHOT_NOT_FOUND`, `SNAPSHOT_INVALID_REF`, `SNAPSHOT_REMOTE_ERROR`.

**`lstk snapshot remove`** — `data`: confirmation of deletion.
```json
{"podName": "my-baseline", "removed": true}
```
Codes: `AUTH_REQUIRED`, `SNAPSHOT_NOT_FOUND`, `SNAPSHOT_INVALID_REF`, `CONFIRMATION_REQUIRED`, `SNAPSHOT_REMOTE_ERROR`.

### Misc

**`lstk update`** — `data`: differs slightly between `--check` and an applied update.
```json
{"currentVersion": "2.2.1", "latestVersion": "2.3.0", "updateAvailable": true}
```
```json
{"currentVersion": "2.2.1", "updatedVersion": "2.3.0", "updated": true, "method": "homebrew"}
```
Codes: `NETWORK_ERROR` (GitHub API unreachable), `INTERNAL_ERROR` (archive/extraction failure).

## Risks / Trade-offs

- **[Risk]** A command's `data` shape needs a breaking change later (e.g. a field's type changes). → Mitigation: `schemaVersion` exists precisely for this; bump it and document the change in the command's help text and changelog. Additive fields (new optional keys) never require a bump.
- **[Risk]** Classifying every reachable `ErrorEvent`/bare-`error` call site per JSON-capable command is real, distributed work — easy to miss one and silently fall back to `INTERNAL_ERROR` where a more specific code exists. → Mitigation: tasks.md sequences this per command (not all at once), and each command's task includes an integration test asserting every documented error path in its catalog entry produces the expected `error.code`, not just `INTERNAL_ERROR`.
- **[Risk]** The `status` behavior change (report all emulators instead of failing fast) is a real behavior difference, even though scoped to `--json` only. → Mitigation: explicitly called out in the proposal and covered by its own spec scenario, so it can't land silently.
- **[Trade-off]** NDJSON for `logs --follow` means the two `logs` modes (bounded vs. follow) don't share one wire shape. → Accepted: a genuinely unbounded stream and a genuinely bounded result are different enough contracts that forcing them into one shape would be more confusing than two clearly-documented ones.
- **[Risk]** `retryable`'s per-code classification is a judgment call (e.g. `EMULATOR_START_FAILED` could plausibly go either way) and could be second-guessed or need revision as real usage surfaces edge cases. → Mitigation: it's a static, centrally-defined mapping in one table (`error-codes`), not scattered per call site, so revising it later is a one-line change per code, not a hunt across the codebase.
- **[Risk]** Naming new types for a YAML future that "shouldn't be implemented yet" could turn out to guess wrong about what YAML support actually needs once it's real (e.g. the streaming variant, per the Open Questions note below, clearly won't just reuse `EnvelopeSink` as-is). → Mitigation: the rename costs nothing today (no shipped code depends on the old names) and only touches the accumulation/naming layer, not behavior; if the guess is wrong, it's a rename again, not a redesign — the actual JSON behavior being built here doesn't change either way.

## Migration Plan

No migration for existing users — `--json` remains rejected for any command not yet opted in, exactly as it is today, until that command's task in tasks.md lands. Rollout is per-command and independently shippable/revertable (each command's opt-in is a single annotation plus a sink-selection branch), so there is no all-or-nothing cutover. No data formats, config files, or stored state change.

`stop`, `reset`, and `update` are implemented first as a pilot (tasks.md section 3), ahead of the rest of the command surface. All three depend only on the section 1-2 infrastructure, not on each other or on any other command, so pulling them forward doesn't reorder anything load-bearing — and together they're a deliberately small, diverse first exercise of the shared plumbing: `stop` is the baseline array-of-emulators success case, `reset` is the first real path through `CONFIRMATION_REQUIRED` and the exit-code-`3` reservation, and `update` is the only one of the three with no Docker/emulator involvement at all, exercising `NETWORK_ERROR` and `retryable: true`. If the `EnvelopeSink`/error-boundary design needs revision after real implementation, this is the cheapest point to catch it — before the remaining ~20 commands are built on top of it.

## Open Questions

- **Should `start`, `restart`, `snapshot load`, and `snapshot save` stream NDJSON progress instead of staying silent until the final envelope?** This is the biggest open fork from the Prior Art review: Terraform's `apply -json` and Pulumi's engine events emit live `pulling`/`applying`/`provisioning`-style lines throughout a slow operation, ending in a final result line; today's design (see Decisions) discards that progress entirely under `--json`, matching what plain-text non-interactive mode already does but potentially leaving a script watching a slow image pull with no output for the whole duration. Deferred rather than decided here because it would introduce a third wire shape (NDJSON-with-trailing-result, distinct from both the single envelope and the pure `logs --follow` stream) and should be scoped as its own decision once there's a concrete case (e.g. CI logs showing an `lstk start --json` step that looks hung) rather than spec'd speculatively. If pursued, `start`/`restart` are the natural pilot given they already have the richest interactive-mode progress today (`ContainerStatusEvent`, `ProgressEvent`) that JSON mode currently drops on the floor.
- Should `warnings` also surface non-fatal issues from *plain-text* mode today (e.g. the "multiple emulators of the same type" message in `start`), or only ones specifically worth a script's attention? Leaning toward: any `MessageEvent{Severity: SeverityWarning}` reachable from a JSON-capable command's path qualifies, decided per command during its implementation task rather than up front.
- Should `snapshot list`/`show` cache platform responses to reduce load when scripts poll `--json` in a loop? Out of scope here; revisit if it becomes a real usage pattern.
- Whether `az start-interception`/`stop-interception` belong in the first implementation wave given they mutate global `~/.azure` state — flagged in tasks.md as a candidate for a later phase rather than blocking the rest.
- If sparse fieldset selection (declined for v1, see Non-Goals) ever becomes worth revisiting, would it be a per-command `--json-fields` flag or a generic post-processing convention (e.g. documenting that `jq` handles this already, so lstk doesn't need to)?
- **The NDJSON shape for `logs --follow` won't just gain a YAML sibling by swapping the marshaler.** Unlike the single-envelope case (where `EnvelopeSink` + a different `Format` is designed to be enough), YAML has no equivalent to "one compact object per line" — its multi-document convention uses `---` separators, a structurally different streaming contract. Whoever proposes YAML support will need a real design decision here, not an assumption that it falls out of the naming work done in this change.
