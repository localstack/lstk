## MODIFIED Requirements

### Requirement: Commands without JSON support reject --json, using the interactive error style
A built-in command that has not been explicitly marked as supporting `--json` output SHALL reject the flag with a non-zero exit and an error naming the command, rather than accepting it and silently rendering plain-text output. JSON support is an explicit per-command opt-in; every built-in command not covered by the `json-command-output` capability rejects `--json`. Proxy commands are rejected via this same mechanism, but only for `--json` in the pre-command-name position (see "Proxy commands reject --json before the command name"); extension dispatch remains exempt entirely (see "Extension dispatch is exempt from JSON support rejection"), since it has no lstk-rendered output to reject on behalf of.

The rejection SHALL render differently depending on whether `--json` itself was the flag that triggered it:

- When `--json` was set (the invocation explicitly asked for JSON), the rejection SHALL render as the standard JSON envelope (see the `output-envelope` capability) with `error.code = "NOT_JSON_CAPABLE"` and a `message` naming the command, written to stdout, exit code `1`.
- The plain-text interactive error style (a title plus a `See help: lstk -h` action, written to stderr) is retained for any other case where a command's output would otherwise not be renderable — this proposal's change is scoped to the `--json`-requested case only.

#### Scenario: Unsupported command with --json set renders a JSON rejection
- **WHEN** `lstk <command> --json` is run for a command that has not implemented JSON output
- **THEN** lstk exits `1`
- **AND** stdout contains a JSON envelope with `"status": "error"` and `"error": {"code": "NOT_JSON_CAPABLE", ...}` naming the command
- **AND** the command's normal work is never performed

#### Scenario: A command that stays JSON-incapable renders a JSON rejection
- **WHEN** `lstk login --json` is run (`login` is not annotated as JSON-capable; see the `json-command-output` capability)
- **THEN** it is rejected the same way as any other unsupported command, naming "login", rendered as a JSON envelope

Note: the default (no-subcommand) invocation and `lstk start` both become JSON-capable as part of this change (see `json-command-output`'s "Per-command JSON opt-in" requirement), so `lstk --json` with no subcommand no longer hits this rejection path — it is covered instead by the emulator-lifecycle requirements in `json-command-output`.
