## ADDED Requirements

### Requirement: Common JSON result envelope
Every command annotated as JSON-capable SHALL, when `--json` is set, write exactly one JSON object to stdout with the fields `schemaVersion` (integer), `command` (string, the canonical command path), `status` (`"ok"` or `"error"`), `data` (object or `null`), `warnings` (array, always present, possibly empty), and `error` (object or `null`). `data` SHALL be `null` when `status` is `"error"`, and `error` SHALL be `null` when `status` is `"ok"`. Both keys SHALL always be present regardless of status.

#### Scenario: Successful command emits an ok envelope
- **WHEN** a JSON-capable command completes successfully with `--json` set
- **THEN** stdout contains one JSON object with `"status": "ok"`, a non-null `"data"`, and `"error": null`

#### Scenario: Failed command emits an error envelope
- **WHEN** a JSON-capable command fails with `--json` set
- **THEN** stdout contains one JSON object with `"status": "error"`, `"data": null`, and a non-null `"error"` object

#### Scenario: Warnings array is always present
- **WHEN** a JSON-capable command completes successfully with no advisory notices
- **THEN** the envelope's `"warnings"` field is an empty array, not `null` and not omitted

### Requirement: A JSON-capable command never emits unstructured output on stdout
When `--json` is set for a command carrying the JSON-capable annotation, lstk SHALL NOT write any plain-text line, spinner, or table to stdout — only the single envelope (or, for a command documented as streaming, the NDJSON stream defined below). An error that has no specific classification SHALL still produce a well-formed envelope, using `error.code = "INTERNAL_ERROR"` as the last-resort fallback, rather than falling through to an unstructured `Error: %v` line.

#### Scenario: Unclassified error still produces a valid envelope
- **WHEN** a JSON-capable command with `--json` set encounters an error that has not been given a specific `error.code`
- **THEN** stdout contains one well-formed JSON envelope with `"status": "error"` and `"error": {"code": "INTERNAL_ERROR", ...}`
- **AND** no unstructured text is written to stdout

#### Scenario: No presentational output leaks through
- **WHEN** a JSON-capable command with `--json` set would otherwise show a spinner or progress output in plain-text mode
- **THEN** none of that presentational output appears on stdout; only the final envelope is written

### Requirement: Exit code conventions
lstk SHALL exit `0` when the emitted envelope's `status` is `"ok"`; `2` only for a Cobra-level usage error that could not be attributed to a specific command's `RunE` (see the fallback scenario in "Best-effort JSON rendering of usage errors" below); `3` when a JSON-capable command emits an envelope with `status: "error"` and `error.code` is `"CONFIRMATION_REQUIRED"`; `4` when `error.code` is `"AUTH_REQUIRED"`; and `1` for any other `status: "error"` envelope, including a `USAGE_ERROR` that *was* successfully rendered as JSON. Scripts SHALL use `error.code` from the envelope, not the exit code, for full-granularity branching — the `3`/`4` reservations exist only because those two categories recur across nearly every command and have an obvious, mechanical remediation (`--force`, `lstk login`) a script can act on without parsing stdout first.

#### Scenario: Exit code 0 on success
- **WHEN** a JSON-capable command succeeds with `--json` set
- **THEN** the process exits with status code `0`

#### Scenario: Exit code 1 on a generic command-level error
- **WHEN** a JSON-capable command fails with `--json` set and emits an envelope with `status: "error"` and an `error.code` other than `CONFIRMATION_REQUIRED` or `AUTH_REQUIRED`
- **THEN** the process exits with status code `1`

#### Scenario: Exit code 3 on CONFIRMATION_REQUIRED
- **WHEN** a JSON-capable command fails with `--json` set and emits `error.code: "CONFIRMATION_REQUIRED"`
- **THEN** the process exits with status code `3`

#### Scenario: Exit code 4 on AUTH_REQUIRED
- **WHEN** a JSON-capable command fails with `--json` set and emits `error.code: "AUTH_REQUIRED"`
- **THEN** the process exits with status code `4`

### Requirement: Best-effort JSON rendering of usage errors
When `--json` has already been successfully parsed by the time a Cobra-level flag or argument validation error occurs, lstk SHALL render that error as the standard envelope with `error.code = "USAGE_ERROR"` instead of Cobra's default plain-text usage error. When the malformed input prevents `--json` itself from being recognized (e.g. it appears after the flag that fails to parse), lstk SHALL fall back to the existing plain-text usage error on stderr with exit code `2`.

#### Scenario: Usage error after --json is recognized renders as JSON
- **WHEN** `lstk snapshot load --json --merge=bogus REF` is run (an invalid merge strategy caught before `RunE` starts real work)
- **THEN** stdout contains a JSON envelope with `"error": {"code": "USAGE_ERROR", ...}` and the process exits `1`

#### Scenario: A malformed flag preceding --json falls back to plain text
- **WHEN** an invocation has a flag-parsing error that occurs before Cobra reaches `--json` in the argument list
- **THEN** lstk prints Cobra's plain-text usage error to stderr and exits `2`, without attempting to render JSON

### Requirement: Streaming envelope variant for continuous output
A command explicitly documented as streaming (`logs --follow`) SHALL, under `--json`, write newline-delimited JSON (NDJSON) instead of a single envelope: each line is a compact JSON object with `schemaVersion`, `command`, `type` (`"log"` or `"error"`), and either `data` or `error`. This variant SHALL only apply to commands explicitly documented as streaming; all other JSON-capable commands SHALL use the single-envelope form even when the underlying operation reads multiple lines or records (e.g. bounded `logs` without `--follow` returns them inside `data.lines` in a single envelope).

#### Scenario: Follow mode emits one JSON object per line
- **WHEN** `lstk logs --follow --json` is run
- **THEN** each new log line is written to stdout as its own compact JSON object with `"type": "log"`
- **AND** no single object attempts to represent the entire (unbounded) stream

#### Scenario: Bounded logs use the single envelope
- **WHEN** `lstk logs --json` is run without `--follow`
- **THEN** stdout contains exactly one JSON envelope whose `data.lines` array contains the retrieved log lines
