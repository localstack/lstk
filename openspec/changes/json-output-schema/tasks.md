## 1. Envelope and error-code infrastructure

- [ ] 1.1 Add `internal/output` envelope types: `Envelope`, `Warning`, `EnvelopeError` (including `Retryable bool`), `EnvelopeAction`, and the `ErrorCode` string enum from the `error-codes` spec. Names avoid a `JSON`-specific prefix on purpose (see design.md's naming decisions) — the envelope shape is designed to outlive JSON as the only serialization.
- [ ] 1.1a Add a single source-of-truth table mapping each `ErrorCode` to its static `Retryable` value (per the `error-codes` spec table), consulted wherever an `EnvelopeError` is constructed so `Retryable` is never set ad hoc per call site.
- [ ] 1.2 Add `Code ErrorCode` field to `output.ErrorEvent` (additive; `PlainSink`/`TUISink` ignore it).
- [ ] 1.3 Implement `output.EnvelopeSink` (`Sink`), constructed with an `output.Format` (only `output.FormatJSON` exists today): type-switches on each event, accumulates data-bearing events into `Envelope.Data`, routes `ErrorEvent` into `Envelope.Error` (defaulting `Code` to `INTERNAL_ERROR` when unset), routes `MessageEvent{Severity: SeverityWarning}` into `Envelope.Warnings`, and drops purely presentational events (`SpinnerEvent`, `ContainerStatusEvent`, `ProgressEvent`, `DeferredEvent`'s inner transient events).
- [ ] 1.4 Add `(*EnvelopeSink).Result() Envelope` plus a helper that marshals per the sink's `Format` and writes it once to stdout as compact JSON (the only format implemented now).
- [ ] 1.5 Add `internal/output` NDJSON stream writer (`type: "log"`/`"error"` lines) for the `logs --follow` variant.
- [ ] 1.6 Unit tests: `EnvelopeSink` event-routing table (one test per event type: accumulated into data / routed to warnings / routed to error / dropped).

## 2. Command-boundary wiring

- [ ] 2.1 `cmd/root.go`: change `requireJSONSupport`'s rejection to render as a JSON envelope (`error.code: NOT_JSON_CAPABLE`) on stdout when `cfg.JSON` is true, matching the `json-flag` delta spec; keep the existing plain-text path for the non-`--json` case (unreachable today, kept for defensiveness).
- [ ] 2.2 `cmd/root.go`: add a JSON error boundary wrapper (parallel to `requireJSONSupport`) that guarantees any error returned from a JSON-capable command's `RunE` — including ones without a specific `ErrorCode` — is rendered as a valid envelope with `error.code: INTERNAL_ERROR` as the fallback, never a bare `Error: %v` line.
- [ ] 2.3 `cmd/root.go` / `Execute()`: catch Cobra-level usage errors and render them as `error.code: USAGE_ERROR` envelopes when `--json` was already parsed; otherwise preserve today's plain-text usage error and exit `2`.
- [ ] 2.4 Wire exit-code conventions (`0` ok, `1` generic command-level error, `2` unattributed usage error, `3` for `CONFIRMATION_REQUIRED`, `4` for `AUTH_REQUIRED`) at the single point `Execute()` already computes the process exit code.
- [ ] 2.5 Integration tests: `NOT_JSON_CAPABLE` rejection renders as JSON when `--json` is set; an unclassified error still produces a valid envelope; a usage error after `--json` renders as JSON, one before it does not; exit codes `3`/`4` fire for `CONFIRMATION_REQUIRED`/`AUTH_REQUIRED` respectively and `1` for every other error code.

## 3. Pilot wave — stop, reset, update

Implemented first, ahead of every other command: each depends only on the shared infrastructure in sections 1-2, not on any other command's JSON support, and together they exercise the envelope's main branch points (a data-bearing success, a `CONFIRMATION_REQUIRED`/exit-`3` failure, and a `retryable: true` failure) before the remaining waves build on the same plumbing.

- [ ] 3.1 `stop`: add annotation, `{"emulators": [...]}` data shape, `RUNTIME_UNAVAILABLE` code.
- [ ] 3.2 `reset`: add annotation, `{"emulator": {...}, "reset": true}` data shape, `EMULATOR_NOT_CONFIGURED`/`EMULATOR_NOT_RUNNING`/`CONFIRMATION_REQUIRED`/`RUNTIME_UNAVAILABLE` codes.
- [ ] 3.3 `update`: add annotation, `{"currentVersion", "latestVersion"/"updatedVersion", "updateAvailable"/"updated", "method"}` data shape (both `--check` and applied-update variants), `NETWORK_ERROR` code.
- [ ] 3.4 Integration tests for all three: success payloads, every documented error code, and exit codes `0`/`1`/`3` (this trio has no `AUTH_REQUIRED`/`2` case).
- [ ] 3.5 Before continuing to section 4, sanity-check the section 1-2 infrastructure against these three real implementations — this pilot is its first real exercise, and is the cheapest point to revise the `EnvelopeSink`/error-boundary design if something doesn't fit in practice.

## 4. Read-only / low-risk commands

- [ ] 4.1 `config path`: add annotation, JSON sink branch, `{"path": "..."}` data shape, `CONFIG_NOT_FOUND`/`CONFIG_INVALID` codes.
- [ ] 4.2 `volume path`: add annotation, `{"volumes": [...]}` data shape.
- [ ] 4.3 `status`: add annotation; change the domain loop (`internal/container/status.go`) to report every configured emulator (running or not) instead of failing fast on the first non-running one, gated so plain-text behavior is unchanged; `{"emulators": [...]}` data shape.
- [ ] 4.4 `logs` (bounded): add annotation, `{"lines": [...]}` data shape.
- [ ] 4.5 `logs --follow`: wire the NDJSON stream writer from 1.5.
- [ ] 4.6 Integration tests for each command in this wave, covering both the success payload and its documented error codes.

## 5. Remaining emulator lifecycle

- [ ] 5.1 `start` (both `cmd/start.go` and the root command's bare invocation): add annotation to both, `{"emulators": [...], "snapshotLoaded": ...}` data shape, classify `RUNTIME_UNAVAILABLE`/`AUTH_REQUIRED`/`LICENSE_INVALID`/`LICENSE_UNSUPPORTED_TAG`/`IMAGE_PULL_FAILED`/`EMULATOR_START_FAILED`/`SNAPSHOT_NOT_FOUND`/`VALIDATION_ERROR` at their existing call sites.
- [ ] 5.2 `restart`: add annotation, `{"stopped": [...], "started": [...]}` data shape, reusing 3.1 (`stop`)'s and 5.1 (`start`)'s classification.
- [ ] 5.3 `volume clear`: add annotation, `{"cleared": [...]}` data shape, `EMULATOR_NOT_CONFIGURED`/`CONFIRMATION_REQUIRED` codes.
- [ ] 5.4 Integration tests for each command in this wave, including the `status`-style "report all configured emulators" scenario for `start`.

## 6. Snapshots

- [ ] 6.1 `snapshot save`: add annotation, `{"kind", "location", "podName", "version", "services", "sizeBytes"}` data shape across local/pod/S3 destinations, classify `AUTH_REQUIRED`/`CREDENTIALS_MISSING`/`SNAPSHOT_BUCKET_NOT_FOUND`/`SNAPSHOT_REMOTE_ERROR`/`VALIDATION_ERROR`.
- [ ] 6.2 `snapshot load`: add annotation, `{"source", "services", "emulatorStarted", "mergeStrategy"}` data shape, classify `SNAPSHOT_NOT_FOUND`/`SNAPSHOT_INVALID_REF`/`SNAPSHOT_REMOTE_ERROR`/`SNAPSHOT_BUCKET_NOT_FOUND`/`CREDENTIALS_MISSING`/`EMULATOR_START_FAILED`.
- [ ] 6.3 `snapshot list`: add annotation, `{"location", "snapshots": [...]}` data shape (platform and S3).
- [ ] 6.4 `snapshot show`: add annotation, data shape mirroring `SnapshotShownEvent` fields directly.
- [ ] 6.5 `snapshot remove`: add annotation, `{"podName", "removed"}` data shape, `CONFIRMATION_REQUIRED` code.
- [ ] 6.6 Integration tests for each command in this wave, covering local/pod/S3 variants where applicable.

## 7. Auth and setup

- [ ] 7.1 `logout`: add annotation, `{"loggedOut", "stillRunning": [...]}` data shape, preserving today's idempotent already-logged-out success.
- [ ] 7.2 `setup aws`: add annotation, `{"profile", "written", "dnsOk"}` data shape, `CONFIRMATION_REQUIRED` code.
- [ ] 7.3 `setup azure`: add annotation, `{"configDir", "cloudRegistered"}` data shape, `DEPENDENCY_MISSING`/`DNS_RESOLUTION_REQUIRED` codes.
- [ ] 7.4 Integration tests for each command in this wave.

## 8. Azure interception (global-state commands)

- [ ] 8.1 `az start-interception`: add annotation, `{"cloudRegistered", "endpoint"}` data shape.
- [ ] 8.2 `az stop-interception`: add annotation, `{"switchedFrom", "switchedTo", "changed"}` data shape, `VALIDATION_ERROR` code for an unrecognized `--cloud`.
- [ ] 8.3 Integration tests for both, run serially (they mutate global `~/.azure` state, matching the existing test isolation approach for this area).

## 9. Docs and cross-cutting verification

- [ ] 9.1 Write `docs/structured-output.md`, the single developer-facing reference for the envelope contract, for engineers to review directly. Named without a `json-` prefix since the envelope/error-code shape it documents is designed to extend to other output formats later, even though JSON is the only one implemented now (state this explicitly in the doc's opening paragraph). Sections: (1) the envelope's fixed fields and an annotated success/error example; (2) the full `error.code` table with each code's `retryable` value; (3) the exit-code table (`0`/`1`/`2`/`3`/`4`); (4) the NDJSON streaming variant used by `logs --follow`; (5) the **entire Command Catalog from design.md, reproduced in full** — every JSON-capable command, its complete `data` shape and example JSON, and its error codes, not a condensed index; (6) a short "adding JSON support to a command" walkthrough for lstk contributors — the `jsonSupportedAnnotation` opt-in, routing through `output.EnvelopeSink`, and where to classify a new `ErrorEvent.Code`. No target line count — reproducing the full catalog verbatim is expected to push this well past a typical reference doc's length, and that's the intent: one document engineers can review and comment on end to end, rather than a summary that sends them back to design.md (an ephemeral change artifact that won't exist after this change is archived) for the actual detail. Source of truth for content: this change's design.md (Prior Art, Decisions, Command Catalog) and the `output-envelope`/`error-codes`/`json-command-output` specs.
- [ ] 9.2 Update `lstk docs`-generated command reference and CLAUDE.md's "Output Routing and Events" section to link to `docs/structured-output.md` instead of duplicating its content.
- [ ] 9.3 Add a lint/test that walks the Cobra command tree and asserts every command carrying `jsonSupportedAnnotation` has at least one integration test exercising `--json`.
- [ ] 9.4 Run `make test` and `make test-integration` for the full command surface touched across sections 3-8.
