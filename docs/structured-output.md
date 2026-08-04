# Structured output (`--json`)

`lstk` supports a global `--json` flag that makes a command emit a single, machine-readable JSON object on stdout instead of human-oriented text. This document is the reference for that contract: the envelope every JSON-capable command shares, the enumerated error codes, exit-code conventions, and — because only a subset of commands support `--json` today — exactly which ones do.

The contract described here (a shared envelope shape, a shared error-code vocabulary) is deliberately **not named after JSON specifically** (`EnvelopeSink`, not `JSONSink`; `output-envelope`, not `json-envelope`). JSON is the only serialization implemented today, but the envelope's shape and the accumulation logic that builds it have nothing JSON-specific about them, which keeps the door open to other formats later without a redesign.

## Implementation status

`--json` support is being rolled out per command, not all at once. The [Command Catalog](#command-catalog) below is split into two parts:

- **[Implemented in this PR](#implemented-in-this-pr)** — `stop`, `reset`, `update`. These accept `--json` today and produce exactly the shapes documented below.
- **[Proposed for future work](#proposed-for-future-work-draft)** — every other built-in command. Attempting `--json` on any of these today is rejected with `NOT_JSON_CAPABLE`. This part is a **first-draft proposal only** — see the warning at the top of that section before relying on any of it.

## The envelope

Every JSON-capable command writes **exactly one** JSON object to stdout (the exception is `logs --follow`, a genuinely unbounded stream — see "Streaming output" below). The shape is:

```jsonc
{
  "schemaVersion": 1,
  "command": "stop",
  "status": "ok",       // "ok" | "error"
  "data": { /* ... */ }, // non-null iff status is "ok"
  "warnings": [],        // always an array, possibly empty
  "error": null          // non-null iff status is "error"
}
```

| Field | Type | Notes |
|---|---|---|
| `schemaVersion` | integer | The envelope's wire-format version, currently always `1`. Not versioned per-command — bumped only on a breaking change to the envelope itself or to a specific command's `data` shape (additive fields never require a bump). A script should check it once: `if (resp.schemaVersion !== 1) { /* bail or adapt */ }`. |
| `command` | string | The canonical command path that produced this envelope (e.g. `"stop"`, `"snapshot show"`). |
| `status` | string | `"ok"` or `"error"` — the one field to branch on for success/failure before looking at anything else. |
| `data` | object or `null` | The command-specific result, documented per command in the [Command Catalog](#command-catalog) below. `null` exactly when `status` is `"error"`; a non-null object otherwise, even if that object is empty. |
| `warnings` | array | Non-fatal notices, always present — an empty array on success with nothing to report, never `null` or omitted. Each entry is `{"code": string, "message": string}`. |
| `error` | object or `null` | The machine-readable failure, documented in [Error object fields](#error-object-fields) below. `null` exactly when `status` is `"ok"`. |

Both `data` and `error` are always present as keys — `null` rather than omitted — so a script can read `resp.data` / `resp.error` without an existence check first.

### Success example

```json
{
  "schemaVersion": 1,
  "command": "stop",
  "status": "ok",
  "data": {
    "emulators": [
      {"type": "aws", "name": "localstack-aws", "wasRunning": true}
    ]
  },
  "warnings": [],
  "error": null
}
```

### Error example

```json
{
  "schemaVersion": 1,
  "command": "reset",
  "status": "error",
  "data": null,
  "warnings": [],
  "error": {
    "code": "CONFIRMATION_REQUIRED",
    "category": "USAGE",
    "message": "reset requires confirmation; use --force to skip in non-interactive mode",
    "retryable": false
  }
}
```

### Error object fields

Within the main payload, the `error` field contains the following sub-fields:

| Field | Type | Notes |
|---|---|---|
| `code` | string | One of the enumerated codes below. Never free text — see the error-codes table. The primary, stable identifier — branch on this for anything specific. |
| `category` | string | One of 7 coarse groupings of `code` (`RUNTIME`, `EMULATOR`, `AUTH`, `RESOURCE`, `CONFIG`, `USAGE`, `INTERNAL`) — see [Error categories](#error-categories) below. Additive alongside `code`, not a replacement for it: a caller that only wants broad handling can switch on `category`'s ~7 values instead of `code`'s ~28, while a caller that already keys off a specific `code` is unaffected. |
| `message` | string | Human-readable headline, informational only. **Not guaranteed stable across versions** — scripts must branch on `code`, not `message`. |
| `retryable` | bool | A static property of `code` (not computed per failure) — see below. Note this is independent of `category`: a category can contain both retryable and non-retryable codes (e.g. `RUNTIME` contains both `NETWORK_ERROR` [retryable] and `DNS_RESOLUTION_REQUIRED` [not]), so `retryable` can't be inferred from `category` alone. |
| `details` | object | Optional, code-specific structured context. Omitted when empty. Illustrative example, for a future `SNAPSHOT_BUCKET_NOT_FOUND`: `{"bucket": "my-terraform-state"}`. Also where the additional diagnostic depth plain text and the TUI show alongside the `message` headline lands, as `summary`/`detail` string keys, when available — e.g. `{"summary": "cannot connect to Docker daemon: ..."}` for `RUNTIME_UNAVAILABLE`. |
| `actions` | array | Optional suggested remediations. For example — this is exactly what `reset` emits today for `EMULATOR_NOT_RUNNING`: `[{"id": "start-localstack", "command": "lstk"}, {"id": "see-help", "command": "lstk -h"}]`. |

## Error codes

Every `error.code` is one of the following fixed constants. A failure that doesn't map to a specific code falls back to `INTERNAL_ERROR` rather than inventing a new, undocumented string.

`retryable: true` means the identical invocation might succeed later without changing anything (a transient runtime/network hiccup) — a polling script can back off and retry. `retryable: false` means retrying the exact same invocation will keep failing until something about the request, config, or environment changes.

| Code | Meaning | Retryable | Category |
|---|---|---|---|
| `RUNTIME_UNAVAILABLE` | The container runtime (Docker today; the abstraction also anticipates Podman, Rancher Desktop, Finch, or Kubernetes, though only Docker is implemented) is unreachable or unhealthy | Yes | `RUNTIME` |
| `IMAGE_PULL_FAILED` | Pulling the emulator image failed and no usable local image exists | Yes | `RUNTIME` |
| `EMULATOR_NOT_RUNNING` | The targeted emulator is not currently running | No | `EMULATOR` |
| `EMULATOR_ALREADY_RUNNING` | An emulator is already running where the command expected it not to be | No | `EMULATOR` |
| `EMULATOR_WRONG_TYPE` | The command requires a specific emulator type but a different one is configured/running | No | `EMULATOR` |
| `EMULATOR_NOT_CONFIGURED` | No container of the requested type exists in the resolved config | No | `EMULATOR` |
| `EMULATOR_START_FAILED` | The emulator failed to reach a healthy state after starting | Yes | `EMULATOR` |
| `AUTH_REQUIRED` | The operation needs a LocalStack auth token and none is available | No | `AUTH` |
| `AUTH_LOGIN_FAILED` | An authentication flow failed | Yes | `AUTH` |
| `CREDENTIALS_MISSING` | Required third-party credentials (e.g. AWS credentials for an S3 remote) could not be resolved | No | `AUTH` |
| `LICENSE_INVALID` | The platform rejected the configured license/token | No | `AUTH` |
| `LICENSE_UNSUPPORTED_TAG` | The configured image tag is not covered by the license | No | `AUTH` |
| `SNAPSHOT_NOT_FOUND` | The referenced snapshot does not exist | No | `RESOURCE` |
| `SNAPSHOT_INVALID_REF` | The snapshot reference could not be parsed | No | `RESOURCE` |
| `SNAPSHOT_REMOTE_ERROR` | A platform or S3 remote call failed | Yes | `RESOURCE` |
| `SNAPSHOT_BUCKET_NOT_FOUND` | The pre-flight S3 bucket-existence check failed | No | `RESOURCE` |
| `CONFIG_INVALID` | The config file failed to parse or validate | No | `CONFIG` |
| `CONFIG_NOT_FOUND` | An explicit config path does not exist | No | `CONFIG` |
| `INTEGRATION_NOT_SET_UP` | A required one-time setup step (e.g. `lstk setup azure`) has not been run | No | `CONFIG` |
| `DEPENDENCY_MISSING` | A required external CLI (e.g. `az`) is not on `PATH` | No | `RUNTIME` |
| `DNS_RESOLUTION_REQUIRED` | A required hostname pattern does not resolve | No | `RUNTIME` |
| `CONFIRMATION_REQUIRED` | A destructive action needs `--force` outside an interactive terminal | No | `USAGE` |
| `VALIDATION_ERROR` | A semantically invalid combination of flags/arguments was given | No | `USAGE` |
| `USAGE_ERROR` | Cobra-level flag or argument parsing failed | No | `USAGE` |
| `NOT_JSON_CAPABLE` | The requested command has not been annotated as JSON-capable yet | No | `USAGE` |
| `NETWORK_ERROR` | An unclassified network/transport failure occurred | Yes | `RUNTIME` |
| `CANCELLED` | The operation was interrupted (e.g. context cancellation via Ctrl+C) | Yes | `INTERNAL` |
| `INTERNAL_ERROR` | Unclassified or unexpected failure; the universal fallback | No | `INTERNAL` |

### Error categories

`error.category` groups the 28 codes above into 7 buckets, additive alongside `code` — it exists purely so a caller that only wants coarse handling doesn't have to build and maintain its own mapping from all 28 codes. `code` is unaffected and remains the primary, stable identifier for anything more specific.

```
RUNTIME    RUNTIME_UNAVAILABLE, IMAGE_PULL_FAILED, DEPENDENCY_MISSING,
           DNS_RESOLUTION_REQUIRED, NETWORK_ERROR
           → something outside lstk's control (Docker, network, a missing binary)

EMULATOR   EMULATOR_NOT_RUNNING, EMULATOR_ALREADY_RUNNING, EMULATOR_WRONG_TYPE,
           EMULATOR_NOT_CONFIGURED, EMULATOR_START_FAILED
           → the emulator isn't in the state this command needs

AUTH       AUTH_REQUIRED, AUTH_LOGIN_FAILED, CREDENTIALS_MISSING,
           LICENSE_INVALID, LICENSE_UNSUPPORTED_TAG
           → identity, credentials, or license/entitlement

RESOURCE   SNAPSHOT_NOT_FOUND, SNAPSHOT_INVALID_REF, SNAPSHOT_REMOTE_ERROR,
           SNAPSHOT_BUCKET_NOT_FOUND
           → a referenced thing (by name or ref) doesn't exist or is invalid
           (named generically, not e.g. SNAPSHOT, so a future non-snapshot
           resource error has somewhere to land without a new category)

CONFIG     CONFIG_INVALID, CONFIG_NOT_FOUND, INTEGRATION_NOT_SET_UP
           → lstk's own configuration is the problem

USAGE      CONFIRMATION_REQUIRED, VALIDATION_ERROR, USAGE_ERROR,
           NOT_JSON_CAPABLE
           → the invocation itself needs to change

INTERNAL   CANCELLED, INTERNAL_ERROR
           → catch-all: unexpected failure, or a user-initiated interruption
```

Every code maps to exactly one category, and the mapping is static — the same code always reports the same category, regardless of which command emitted it (see the Category column above for the authoritative per-code mapping).

## Exit codes

```
0   status: "ok"
1   status: "error", any code other than the two below
2   a Cobra-level usage error that occurred before --json could be recognized
    (falls back to today's plain-text usage error on stderr, not an envelope)
3   error.code == "CONFIRMATION_REQUIRED"
4   error.code == "AUTH_REQUIRED"
```

Scripts needing full granularity should read `error.code` from the envelope, not the exit code — a 28-entry enum doesn't fit in a POSIX exit code. The two reservations exist because `CONFIRMATION_REQUIRED` and `AUTH_REQUIRED` recur across nearly every command and have an obvious, mechanical remediation (`--force`, `lstk login`) a script can act on without parsing stdout first.

A `USAGE_ERROR` that *was* successfully rendered as an envelope (because `--json` had already been parsed before the failure) exits `1`, not `2` — exit `2` is reserved specifically for the case where `--json` itself couldn't be recognized yet (e.g. a malformed flag appearing before `--json` in the invocation), so no envelope was possible at all.

## Streaming output

`logs --follow` is the one command whose output is a genuinely unbounded stream — there's no natural moment to close a single JSON object around a `tail -f`-style operation. Under `--json --follow`, each line is its own compact JSON object, newline-delimited (NDJSON), with a `type` field instead of `status`. Unlike every other example in this document, this one is shown compact and single-line deliberately — that's the actual wire format, not a formatting shortcut; pretty-printing it would misrepresent NDJSON as something else:

```json
{"schemaVersion":1,"command":"logs","type":"log","data":{"source":"emulator","level":"info","line":"Ready."}}
```

`type` is `"log"` for a line or `"error"` if the stream itself fails. Bounded `logs --json` (without `--follow`) uses the normal single-envelope shape instead, with the same `{source, level, line}` objects collected into `data.lines` — it terminates naturally, so the standard envelope fits.

🕐 Planned — `logs` does not yet accept `--json`.

## Command Catalog

There are many commands supported by `lstk`, but they'll be addressed in phases. Initially we've focused on `stop`, `reset`, and `update` commands, simply to test the generation of JSON output. The remaining commands will follow in later work, where their specific JSON schema will be considered in more depth (for now, they're simply a rough proposal)

### Implemented in this PR

These three ship in this PR with `--json` support. The shapes below are real — they match what the code actually produces, not a proposal.

**`lstk stop`** — which configured emulators were actually running and got stopped.
```json
{
  "schemaVersion": 1,
  "command": "stop",
  "status": "ok",
  "data": {
    "emulators": [
      {"type": "aws", "name": "localstack-aws", "wasRunning": true}
    ]
  },
  "warnings": [],
  "error": null
}
```
Codes: `RUNTIME_UNAVAILABLE`, `EMULATOR_NOT_RUNNING` (today, `stop` fails fast on the first configured emulator found not running, matching plain-text behavior), `CONFIG_INVALID`, `CONFIG_NOT_FOUND` (bad or missing `--config` path).

**`lstk reset`** — confirmation of what was reset (AWS-only today).
```json
{
  "schemaVersion": 1,
  "command": "reset",
  "status": "ok",
  "data": {
    "emulator": {"type": "aws", "name": "localstack-aws"},
    "reset": true
  },
  "warnings": [],
  "error": null
}
```
Codes: `EMULATOR_NOT_CONFIGURED` (no AWS container configured), `EMULATOR_NOT_RUNNING`, `CONFIRMATION_REQUIRED` (no `--force` outside a TTY, exit code `3`), `RUNTIME_UNAVAILABLE`, `CONFIG_INVALID`, `CONFIG_NOT_FOUND` (bad or missing `--config` path).

**`lstk update`** — differs between `--check` and an applied update.
```json
{
  "schemaVersion": 1,
  "command": "update",
  "status": "ok",
  "data": {
    "currentVersion": "2.2.1",
    "latestVersion": "2.3.0",
    "updateAvailable": true
  },
  "warnings": [],
  "error": null
}
```
```json
{
  "schemaVersion": 1,
  "command": "update",
  "status": "ok",
  "data": {
    "currentVersion": "2.2.1",
    "updatedVersion": "2.3.0",
    "updated": true,
    "method": "homebrew"
  },
  "warnings": [],
  "error": null
}
```
Codes: `NETWORK_ERROR` (GitHub API unreachable), `INTERNAL_ERROR` (archive download verification, extraction, or replacement failure), `CONFIG_INVALID`, `CONFIG_NOT_FOUND` (bad or missing `--config` path).

### Proposed for future work (draft)

> **This section is a first-draft proposal only, not a committed contract.** None of the commands below accept `--json` yet — every one of them is rejected with `NOT_JSON_CAPABLE` today. The shapes shown are a starting point for design discussion, included here in full so the whole intended surface can be reviewed at once rather than piecemeal across many small follow-up PRs. Expect fields, error codes, and possibly the overall approach for any of these to change based on human feedback before implementation — treat everything below as a proposal to critique, not a spec to build against.

#### Emulator lifecycle

**`lstk start`** — one emulator entry per configured container, plus whether a configured snapshot was auto-loaded.
```json
{
  "schemaVersion": 1,
  "command": "start",
  "status": "ok",
  "data": {
    "emulators": [
      {"type": "aws", "name": "localstack-aws", "host": "localhost:4566", "version": "3.9.0", "alreadyRunning": false, "persist": false}
    ],
    "snapshotLoaded": null
  },
  "warnings": [],
  "error": null
}
```
Codes: `RUNTIME_UNAVAILABLE`, `AUTH_REQUIRED`, `LICENSE_INVALID`, `LICENSE_UNSUPPORTED_TAG`, `IMAGE_PULL_FAILED`, `EMULATOR_START_FAILED`, `SNAPSHOT_NOT_FOUND` (bad `--snapshot`), `VALIDATION_ERROR` (`--snapshot` with `--no-snapshot`).

**`lstk restart`** — the stop result and the start result, reusing both shapes above.
```json
{
  "schemaVersion": 1,
  "command": "restart",
  "status": "ok",
  "data": {
    "stopped": [
      {"type": "aws", "name": "localstack-aws", "wasRunning": true}
    ],
    "started": [
      {"type": "aws", "name": "localstack-aws", "host": "localhost:4566", "version": "3.9.0", "alreadyRunning": false, "persist": false}
    ]
  },
  "warnings": [],
  "error": null
}
```
Codes: `RUNTIME_UNAVAILABLE`, `AUTH_REQUIRED`, `LICENSE_INVALID`, `EMULATOR_START_FAILED`.

**`lstk status`** — one entry per *configured* emulator, not just running ones: unlike today's plain-text behavior (which stops at the first non-running emulator), JSON mode reports every configured emulator's running/not-running state. Non-running entries omit the detail fields entirely rather than nulling them out.
```json
{
  "schemaVersion": 1,
  "command": "status",
  "status": "ok",
  "data": {
    "emulators": [
      {
        "type": "aws", "running": true, "name": "localstack-aws", "version": "3.9.0",
        "host": "localhost:4566", "uptimeSeconds": 1234, "persistence": false,
        "resourceSummary": {"resources": 12, "services": 4},
        "resources": [{"service": "s3", "name": "my-bucket", "region": "us-east-1", "account": "000000000000"}]
      },
      {"type": "snowflake", "running": false}
    ]
  },
  "warnings": [],
  "error": null
}
```
Codes: `RUNTIME_UNAVAILABLE`.

**`lstk logs`** (bounded) — `data.lines` is the same `{source, level, line}` shape used by the NDJSON stream variant (see "Streaming output" above).
```json
{
  "schemaVersion": 1,
  "command": "logs",
  "status": "ok",
  "data": {
    "lines": [
      {"source": "emulator", "level": "info", "line": "Ready."}
    ]
  },
  "warnings": [],
  "error": null
}
```
Codes: `RUNTIME_UNAVAILABLE`, `EMULATOR_NOT_RUNNING`.

**`lstk volume path`** — one path per configured container.
```json
{
  "schemaVersion": 1,
  "command": "volume path",
  "status": "ok",
  "data": {
    "volumes": [
      {"type": "aws", "path": "/Users/x/Library/Caches/lstk/aws"}
    ]
  },
  "warnings": [],
  "error": null
}
```
Codes: `CONFIG_INVALID`.

**`lstk volume clear`** — which volumes were actually cleared.
```json
{
  "schemaVersion": 1,
  "command": "volume clear",
  "status": "ok",
  "data": {
    "cleared": [
      {"type": "aws", "path": "/Users/x/Library/Caches/lstk/aws"}
    ]
  },
  "warnings": [],
  "error": null
}
```
Codes: `EMULATOR_NOT_CONFIGURED` (bad `--type`), `CONFIRMATION_REQUIRED` (no `--force` outside a TTY).

#### Configuration and auth

**`lstk config path`** — the resolved (or explicitly `--config`-overridden) path.
```json
{
  "schemaVersion": 1,
  "command": "config path",
  "status": "ok",
  "data": {
    "path": "/Users/x/.config/lstk/config.toml"
  },
  "warnings": [],
  "error": null
}
```
Codes: `CONFIG_NOT_FOUND` (`--config` path doesn't exist), `CONFIG_INVALID`.

**`lstk logout`** — whether there was anything to log out of, and any emulators still running with the now-removed token.
```json
{
  "schemaVersion": 1,
  "command": "logout",
  "status": "ok",
  "data": {
    "loggedOut": true,
    "stillRunning": []
  },
  "warnings": [],
  "error": null
}
```
When already logged out, this is still `status: "ok"` with `"loggedOut": false` — logout is idempotent, and JSON mode preserves that rather than inventing a new error for it. No codes expected in normal operation; `INTERNAL_ERROR` is the universal fallback.

**`lstk setup aws`** — the profile that was written and whether the LocalStack hostname resolved.
```json
{
  "schemaVersion": 1,
  "command": "setup aws",
  "status": "ok",
  "data": {
    "profile": "localstack",
    "written": true,
    "dnsOk": true
  },
  "warnings": [],
  "error": null
}
```
Codes: `CONFIRMATION_REQUIRED` (existing profile differs, no `--force`).

**`lstk setup azure`** — the isolated config dir and the cloud that was registered.
```json
{
  "schemaVersion": 1,
  "command": "setup azure",
  "status": "ok",
  "data": {
    "configDir": "/Users/x/.config/lstk/azure",
    "cloudRegistered": "LocalStack"
  },
  "warnings": [],
  "error": null
}
```
Codes: `DEPENDENCY_MISSING` (`az` CLI not on `PATH`), `RUNTIME_UNAVAILABLE`, `EMULATOR_NOT_RUNNING`, `DNS_RESOLUTION_REQUIRED`.

**`lstk az start-interception`** — same shape as `setup azure`, plus the resolved endpoint.
```json
{
  "schemaVersion": 1,
  "command": "az start-interception",
  "status": "ok",
  "data": {
    "cloudRegistered": "LocalStack",
    "endpoint": "https://azure.localhost.localstack.cloud:4566"
  },
  "warnings": [],
  "error": null
}
```
Codes: `DEPENDENCY_MISSING`, `RUNTIME_UNAVAILABLE`, `EMULATOR_NOT_RUNNING`, `DNS_RESOLUTION_REQUIRED`.

**`lstk az stop-interception`** — what changed, or confirmation nothing did (LocalStack wasn't the active cloud).
```json
{
  "schemaVersion": 1,
  "command": "az stop-interception",
  "status": "ok",
  "data": {
    "switchedFrom": "LocalStack",
    "switchedTo": "AzureCloud",
    "changed": true
  },
  "warnings": [],
  "error": null
}
```
Codes: `VALIDATION_ERROR` (`--cloud` not a registered cloud).

#### Snapshots

**`lstk snapshot save`** — one shape covering local/pod/S3 destinations, discriminated by `kind`.
```json
{
  "schemaVersion": 1,
  "command": "snapshot save",
  "status": "ok",
  "data": {
    "kind": "pod",
    "location": "pod:my-baseline",
    "podName": "my-baseline",
    "version": 4,
    "services": ["s3", "lambda"],
    "sizeBytes": 245678
  },
  "warnings": [],
  "error": null
}
```
(`kind` is `"local"` | `"pod"` | `"s3"`; `podName`/`version` are `null` for `kind: "local"`.) Codes: `RUNTIME_UNAVAILABLE`, `EMULATOR_NOT_RUNNING`, `AUTH_REQUIRED` (pod), `CREDENTIALS_MISSING` (S3, no AWS creds resolvable), `SNAPSHOT_BUCKET_NOT_FOUND`, `SNAPSHOT_REMOTE_ERROR`, `VALIDATION_ERROR` (bad pod name).

**`lstk snapshot load`** — what was loaded and whether it started the emulator to do so.
```json
{
  "schemaVersion": 1,
  "command": "snapshot load",
  "status": "ok",
  "data": {
    "source": "pod:my-baseline",
    "services": ["s3", "lambda"],
    "emulatorStarted": false,
    "mergeStrategy": "account-region-merge"
  },
  "warnings": [],
  "error": null
}
```
Codes: `SNAPSHOT_NOT_FOUND`, `SNAPSHOT_INVALID_REF`, `SNAPSHOT_REMOTE_ERROR`, `SNAPSHOT_BUCKET_NOT_FOUND`, `CREDENTIALS_MISSING`, `AUTH_REQUIRED`, `RUNTIME_UNAVAILABLE`, `EMULATOR_START_FAILED`, `VALIDATION_ERROR` (bad `--merge`).

**`lstk snapshot list`** — the platform or S3 location queried, and the snapshots found.
```json
{
  "schemaVersion": 1,
  "command": "snapshot list",
  "status": "ok",
  "data": {
    "location": "platform",
    "snapshots": [
      {"name": "my-baseline", "version": 4, "lastChanged": "2026-07-01T12:00:00Z"}
    ]
  },
  "warnings": [],
  "error": null
}
```
Codes: `AUTH_REQUIRED` (platform), `CREDENTIALS_MISSING`/`SNAPSHOT_BUCKET_NOT_FOUND` (S3), `SNAPSHOT_REMOTE_ERROR`, `EMULATOR_NOT_RUNNING` (S3 list requires a running emulator).

**`lstk snapshot show`** — mirrors the existing `SnapshotShownEvent` struct directly (the richest structured event in the codebase already).
```json
{
  "schemaVersion": 1,
  "command": "snapshot show",
  "status": "ok",
  "data": {
    "name": "my-baseline", "version": 4, "created": "2026-06-01T00:00:00Z", "sizeBytes": 245678,
    "localstackVersion": "3.9.0", "message": "", "services": ["s3"],
    "resources": [{"service": "s3", "counts": [{"noun": "buckets", "count": 3}]}]
  },
  "warnings": [],
  "error": null
}
```
Codes: `AUTH_REQUIRED`, `SNAPSHOT_NOT_FOUND`, `SNAPSHOT_INVALID_REF`, `SNAPSHOT_REMOTE_ERROR`.

**`lstk snapshot remove`** — confirmation of deletion.
```json
{
  "schemaVersion": 1,
  "command": "snapshot remove",
  "status": "ok",
  "data": {
    "podName": "my-baseline",
    "removed": true
  },
  "warnings": [],
  "error": null
}
```
Codes: `AUTH_REQUIRED`, `SNAPSHOT_NOT_FOUND`, `SNAPSHOT_INVALID_REF`, `CONFIRMATION_REQUIRED`, `SNAPSHOT_REMOTE_ERROR`.

## Commands that will never support `--json`

- **`login`** — requires an interactive terminal unconditionally (browser-based OAuth) and has no defined non-interactive behavior at all today, so there's no output to render as JSON.
- **`-v`/`--version`** — Cobra's built-in version flag is handled before any of lstk's own command dispatch runs at all (`Command.execute()` checks it before `PreRunE`/`RunE`), so there is no hook to intercept it without dropping Cobra's own version mechanism — which would newly couple `--version` to config-file loading, breaking the property (shared with `git --version`/`docker --version`) that a version check should work even against a broken environment. This is a deliberate, permanent limitation, not a gap waiting on a future PR.
- **Proxy commands** (`aws`, `terraform`, `cdk`, `sam`, `az` passthrough) and **extension dispatch** — both already have a settled, separate `--json` contract: `--json` before the proxy command's name is rejected the same as any unsupported command, while `--json` from the command name onward is forwarded to the wrapped tool untouched (Terraform, for instance, has its own real `-json` flag). Extensions receive the resolved `--json` value in their runtime context and decide for themselves.
