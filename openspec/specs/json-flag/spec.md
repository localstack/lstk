# json-flag Specification

## Purpose

Provide a global `--json` flag recognized the same way `--non-interactive` already is: parsed at the root command, exposed to every built-in command via `env.Env`, forcing non-interactive rendering when set, conveyed to `lstk` extensions via their runtime context, and rejected — using lstk's interactive error style — for any built-in command that has not explicitly opted into JSON output. Does not cover any individual command actually emitting JSON; that is out of scope here and lands command by command in later changes.

## Requirements

### Requirement: Global --json flag on built-in commands

The root command SHALL register a persistent `--json` boolean flag, alongside `--non-interactive`, whose resolved value SHALL be exposed to every built-in command via `env.Env`.

#### Scenario: Flag recognized on the root command

- **WHEN** `lstk --json <any built-in command>` is invoked
- **THEN** Cobra accepts the flag without error and `env.Env.JSON` is `true` for that invocation

#### Scenario: Flag defaults to false

- **WHEN** `lstk <any built-in command>` is invoked without `--json`
- **THEN** `env.Env.JSON` is `false`

### Requirement: --json forces non-interactive rendering

When `--json` is set, `lstk` SHALL NOT launch the interactive Bubble Tea TUI and SHALL NOT block waiting on stdin input, regardless of whether stdout is a TTY.

#### Scenario: --json on a TTY skips the TUI

- **WHEN** `lstk start --json` is run attached to an interactive TTY
- **THEN** `lstk` runs the non-interactive start path instead of launching the TUI

#### Scenario: --json alone is sufficient, without --non-interactive

- **WHEN** `lstk start --json` is run without also passing `--non-interactive`
- **THEN** the resulting behavior is the same as passing both `--json` and `--non-interactive`

### Requirement: Proxy commands reject --json before the command name

For each proxy command (`aws`, `terraform`, `cdk`, `sam`, `az`), `lstk` SHALL recognize `--json`/`--json=<value>` when it appears in the raw invocation before that command's own name/alias, and SHALL reject the invocation the same way as an unsupported built-in command when it resolves to `true`. This is the same flag-namespace boundary `--non-interactive`/`--config` already use for proxy commands (before the command name is lstk's own space) — a user typing `--json` there means it for lstk, not for the wrapped tool, and lstk should say so rather than silently forwarding a flag the wrapped tool likely doesn't understand.

#### Scenario: Bare --json before the command name is rejected

- **WHEN** `lstk --json aws <args>` (or the `terraform`/`cdk`/`sam`/`az` equivalent) is run
- **THEN** lstk rejects the invocation the same way as any other unsupported command, naming the proxy command, and the wrapped tool is never invoked

#### Scenario: --json=true before the command name is rejected

- **WHEN** `lstk --json=true aws <args>` (or the `terraform`/`cdk`/`sam`/`az` equivalent) is run
- **THEN** lstk rejects the invocation the same way as the bare-flag case

#### Scenario: --json=false before the command name is not rejected

- **WHEN** `lstk --json=false aws <args>` (or the `terraform`/`cdk`/`sam`/`az` equivalent) is run
- **THEN** lstk does not reject the invocation and does not set `env.Env.JSON`; the wrapped tool runs normally

#### Scenario: A malformed value before the command name is treated as true

- **WHEN** `lstk --json=notabool aws <args>` (or the `terraform`/`cdk`/`sam`/`az` equivalent) is run
- **THEN** lstk rejects the invocation the same way as the bare-flag case, matching the existing `--non-interactive=<value>` malformed-value behavior

### Requirement: Proxy commands forward --json from the command name onward

`lstk` SHALL NOT recognize, strip, or otherwise act on `--json`/`--json=<value>` appearing anywhere from a proxy command's own name onward (inclusive of its wrapped tool's own subcommand and arguments) — it SHALL be left exactly as the user typed it and reach the wrapped tool untouched. Unlike the pre-name position, this part of the invocation is never lstk's flag namespace: a proxy command's output is entirely the wrapped tool's own, and the wrapped tool may define its own meaning for `--json`/`-json` there (as Terraform already does on `plan`/`apply`/`show`) that lstk must not shadow.

#### Scenario: --json immediately after the command name is forwarded, not rejected

- **WHEN** `lstk aws --json <args>` (or the `terraform`/`cdk`/`sam`/`az` equivalent) is run
- **THEN** lstk does not reject the invocation and does not set `env.Env.JSON`; `--json` is forwarded to the wrapped tool as part of `<args>`

#### Scenario: --json after the wrapped tool's own action is forwarded, not rejected

- **WHEN** `lstk terraform plan --json` (or an equivalent trailing position for `cdk`/`sam`/`aws`/`az`) is run
- **THEN** lstk does not reject the invocation and does not set `env.Env.JSON`; `--json` is forwarded to the wrapped tool as part of its own arguments

### Requirement: Commands without JSON support reject --json, using the interactive error style

A built-in command that has not been explicitly marked as supporting `--json` output SHALL reject the flag with a non-zero exit and an error naming the command, rather than accepting it and silently rendering plain-text output. JSON support is an explicit per-command opt-in; no built-in command has opted in as of this change, so every built-in command currently rejects `--json`. Proxy commands are rejected via this same mechanism, but only for `--json` in the pre-command-name position (see "Proxy commands reject --json before the command name"); extension dispatch remains exempt entirely (see "Extension dispatch is exempt from JSON support rejection"), since it has no lstk-rendered output to reject on behalf of.

The rejection SHALL use lstk's established interactive error style — a title plus a `See help: lstk -h` action — rather than a bare title, matching the format already used elsewhere (e.g. `dispatchExtension`'s unknown-command error).

#### Scenario: Unsupported command rejects --json

- **WHEN** `lstk <command> --json` is run for a command that has not implemented JSON output
- **THEN** lstk exits non-zero, renders
  ```
  Error: "<command>" is not able to provide output in JSON format
    ==> See help: lstk -h
  ```
  and the command's normal work is never performed

#### Scenario: Default start behavior rejects --json

- **WHEN** `lstk --json` is run with no subcommand (the default start behavior)
- **THEN** it is rejected the same way as any other unsupported command, naming "start"

### Requirement: Extension dispatch is exempt from JSON support rejection

An unknown command that resolves to an `lstk` extension SHALL NOT be rejected for lacking JSON support — that rejection only applies to commands `lstk` itself owns and renders. The extension still receives the resolved `--json` value via its runtime context and decides for itself whether to honor it.

#### Scenario: Extension invocation is not gated

- **WHEN** `lstk foo --json` is run and `foo` resolves to an extension (not a built-in command)
- **THEN** lstk does not reject the invocation for lacking JSON support; the extension is invoked and receives `"json": true` in its context

### Requirement: --json conveyed to extensions

`lstk` SHALL convey the resolved `--json` value to `lstk` extensions via the extension runtime context (`LSTK_EXT_CONTEXT`), as an additive field that does not require an `LSTK_EXT_API_VERSION` bump.

#### Scenario: Extension receives json in its context

- **WHEN** an extension `lstk-foo` is invoked via `lstk foo --json`
- **THEN** the `LSTK_EXT_CONTEXT` environment variable passed to `lstk-foo` decodes to an object containing `"json": true`

#### Scenario: Extension receives json: false by default

- **WHEN** an extension `lstk-foo` is invoked via `lstk foo` without `--json`
- **THEN** the `LSTK_EXT_CONTEXT` environment variable decodes to an object containing `"json": false`

#### Scenario: --json implies nonInteractive in extension context

- **WHEN** an extension `lstk-foo` is invoked via `lstk foo --json` without also passing `--non-interactive`
- **THEN** the `LSTK_EXT_CONTEXT` payload has both `"json": true` and `"nonInteractive": true`
