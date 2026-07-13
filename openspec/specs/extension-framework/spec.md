# extension-framework Specification

## Purpose

Provide a Git-style extension model so that `lstk <name>` invokes an external `lstk-<name>` executable when `name` is not a built-in command, forwarding arguments, streams, and exit codes, and resolving extensions from the user's `PATH`.

## Requirements

### Requirement: Unknown commands dispatch to extension executables

When a user runs `lstk <name> [args...]` and `<name>` does not match any built-in command or alias, lstk SHALL attempt to resolve and execute an extension executable named `lstk-<name>`. If no such executable is found, lstk SHALL fail with its standard unknown-command error and a non-zero exit code.

Built-in commands SHALL always take precedence over extensions: an extension named `lstk-<name>` SHALL NOT shadow or override a built-in command `<name>`.

#### Scenario: Built-in command takes precedence

- **WHEN** a user runs `lstk start` and a built-in `start` command exists
- **THEN** the built-in command runs
- **AND** no `lstk-start` executable is searched for or executed

#### Scenario: Unknown command resolves to an extension

- **WHEN** a user runs `lstk hello world` and no built-in `hello` command exists but an `lstk-hello` executable is resolvable on `PATH`
- **THEN** lstk executes the `lstk-hello` executable
- **AND** passes `world` as its argument

#### Scenario: Unknown command with no matching extension

- **WHEN** a user runs `lstk doesnotexist` and neither a built-in command nor an `lstk-doesnotexist` executable exists
- **THEN** lstk prints an unknown-command error
- **AND** exits with a non-zero status

### Requirement: Extension resolution order

lstk SHALL resolve `lstk-<name>` executables by searching, in order: (1) the bundled-extensions directory alongside the lstk executable (see the extension-bundling capability), then (2) the directories on the user's `PATH`, using the platform's standard executable lookup. The first match SHALL be used, so a bundled extension takes precedence over a `PATH` executable of the same name. On Windows, platform executable extensions (e.g. `.exe`, `.cmd`, `.bat`) SHALL be honored when resolving the executable name.

#### Scenario: Bundled extension wins over PATH

- **WHEN** an `lstk-deploy` exists both in the bundled-extensions directory and on the user's `PATH`
- **THEN** lstk executes the bundled `lstk-deploy`

#### Scenario: Resolves from PATH when not bundled

- **WHEN** an `lstk-hello` executable exists on the user's `PATH` and not in the bundled-extensions directory
- **THEN** lstk executes it when the user runs `lstk hello`

#### Scenario: Not found anywhere

- **WHEN** no `lstk-hello` executable exists in the bundled-extensions directory or on the user's `PATH`
- **THEN** lstk reports an unknown-command error and exits non-zero

### Requirement: Argument, stream, and exit-code forwarding

When invoking an extension, lstk SHALL forward all arguments that follow `<name>` to the extension executable unmodified, and SHALL NOT attempt to parse or interpret extension-specific flags. lstk's own global flags are recognized only when they appear before `<name>`; everything from `<name>` onward is treated as opaque and forwarded verbatim (see the extension-runtime-context capability for how resolved global flags reach the extension). lstk SHALL pass through the child process's standard input, standard output, and standard error unmodified, and SHALL propagate the child process's exit code as lstk's own exit code.

#### Scenario: Flags after the command name are forwarded, not parsed by lstk

- **WHEN** a user runs `lstk hello --verbose --name=foo`
- **THEN** lstk invokes `lstk-hello` with `--verbose --name=foo`
- **AND** lstk does not error on unknown flags

#### Scenario: Global flags before the command name are consumed by lstk

- **WHEN** a user runs `lstk --non-interactive hello --verbose`
- **THEN** lstk consumes `--non-interactive` itself and invokes `lstk-hello` with only `--verbose`
- **AND** an extension's own flag of the same name appearing after `<name>` is forwarded unchanged

#### Scenario: Exit code is propagated

- **WHEN** the `lstk-hello` extension exits with status 3
- **THEN** lstk exits with status 3
- **AND** lstk does not print an additional lstk-level error message

#### Scenario: Streams are passed through

- **WHEN** an extension reads from stdin and writes to stdout and stderr
- **THEN** the user's terminal stdin/stdout/stderr are connected to the extension unaltered

### Requirement: Extensions are self-describing; no lstk-side manifest

lstk SHALL NOT require a manifest file to discover, validate, or invoke an extension. Any compatibility or requirement checks (for example, a minimum supported contract version, or whether authentication is needed) are the extension's own responsibility: the extension SHALL determine these for itself from the runtime context (notably `LSTK_EXT_API_VERSION`) and SHALL refuse to run when its requirements are not met. lstk SHALL execute any resolvable `lstk-<name>` executable without inspecting metadata about it.

#### Scenario: No manifest required to run

- **WHEN** an `lstk-hello` executable exists on `PATH` with no accompanying metadata file
- **THEN** lstk executes it directly without looking for or parsing a manifest

#### Scenario: Extension self-enforces contract compatibility

- **WHEN** an extension requires a newer runtime-context contract than `LSTK_EXT_API_VERSION` advertises
- **THEN** the extension detects the mismatch from the environment and refuses to run
- **AND** lstk does not perform this check on the extension's behalf

### Requirement: Help and discoverability

lstk SHALL include resolvable extensions in its help output by scanning the bundled-extensions directory and `PATH` for `lstk-*` executables and listing each discovered extension's command name under a distinct "Extensions" grouping, so users can discover installed extensions. When a bundled and a `PATH` extension share a name, the entry SHALL be listed once (the one that would run). Built-in command help SHALL remain unchanged. The Extensions section SHALL align its description column with the built-in command/Tools sections, using the same name-padding rule, so the help output reads as one consistent table.

#### Scenario: Extensions listed in help

- **WHEN** a user runs `lstk --help`, an `lstk-deploy` is bundled, and an `lstk-hello` is on `PATH`
- **THEN** the help output lists both `deploy` and `hello` under an Extensions section

#### Scenario: Extension descriptions align with command descriptions

- **WHEN** a user runs `lstk --help` and a bundled extension with a description is listed
- **THEN** the extension's description begins in the same column as the descriptions of the built-in command sections

### Requirement: One-line descriptions from a bundled descriptions file

lstk SHALL enrich the help listing with a one-line description for bundled extensions by reading a static descriptions file from the bundled directory when present, which maps a bundled extension's command name to its description. (How that file is hand-authored, shipped, and release-validated is specified by the future `add-bundled-extension-distribution` change; this change covers only reading it when present.) lstk SHALL NOT execute any extension to obtain help text; help rendering remains side-effect-free. A bundled extension named in the descriptions file SHALL be listed with that description; a bundled extension absent from the file, and every `PATH`/custom extension, SHALL be listed by command name only. A missing or unreadable descriptions file SHALL degrade to name-only listing without error.

#### Scenario: Bundled extension shows its description

- **WHEN** the descriptions file maps `deploy` to a one-line description and a user runs `lstk help`
- **THEN** lstk lists `deploy` with that description
- **AND** lstk does not execute `lstk-deploy` to render help

#### Scenario: PATH and custom extensions are name-only

- **WHEN** an `lstk-hello` is resolved from `PATH`
- **THEN** lstk lists `hello` by command name with no description
- **AND** lstk does not execute it during help

#### Scenario: Missing descriptions file degrades gracefully

- **WHEN** no descriptions file is present (or it cannot be read)
- **THEN** lstk lists all extensions by command name only
- **AND** help rendering does not error
