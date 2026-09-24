## ADDED Requirements

### Requirement: Command events record whether the invocation requested a wrapped tool

Every `lstk_command` event SHALL carry `parameters.proxied`, true when the invocation asked lstk to run a wrapped external tool on the user's behalf and false otherwise. The value SHALL describe the request, not the outcome: it is true for a proxy invocation that succeeded, for one whose tool exited non-zero, and for one that failed lstk's preflight before the tool ever ran. It SHALL be emitted on every event, false included.

The proxied set SHALL be the proxy commands `aws`, `az`, `cdk`, `sam`, and `terraform`, including their aliases. A command SHALL declare membership explicitly rather than have it inferred from an implementation detail such as flag-parsing configuration, and the same declaration SHALL govern the existing `parameters.subcommand` field so the two cannot disagree.

Subcommands of a proxy command that perform lstk's own work rather than forwarding to the wrapped tool SHALL report `proxied` as false. Extension invocations SHALL report `proxied` as false: an extension is lstk's own code shipped separately, not a wrapped third-party tool, and lstk cannot observe whether the extension in turn wraps one.

#### Scenario: A successful proxy invocation is still proxied

- **WHEN** `lstk aws s3 ls` runs and the AWS CLI exits 0
- **THEN** the event has `parameters.proxied` true
- **AND** `result.proxy_exit_code` is 0

#### Scenario: A proxy invocation that never reached the tool is still proxied

- **WHEN** `lstk aws s3 ls` fails lstk's preflight because the emulator is not running
- **THEN** the event has `parameters.proxied` true
- **AND** `result.proxy_exit_code` is absent

#### Scenario: An alias is proxied like the command it names

- **WHEN** `lstk tf apply` runs
- **THEN** the event has `parameters.proxied` true
- **AND** the command is recorded as `terraform`, not `tf`

#### Scenario: lstk's own commands are not proxied

- **WHEN** `lstk start` runs
- **THEN** the event has `parameters.proxied` false
- **AND** `result.proxy_exit_code` is absent

#### Scenario: lstk's own work under a proxy command is not proxied

- **WHEN** `lstk az start-interception` runs
- **THEN** the event has `parameters.proxied` false
- **AND** the command is recorded under a name distinct from the `az` passthrough's

#### Scenario: Extension invocations are lstk's own

- **WHEN** lstk resolves and runs the extension `deploy` and it exits 7
- **THEN** the `ext:deploy` event has `parameters.proxied` false
- **AND** `result.exit_code` is 7 with no `result.proxy_exit_code`

### Requirement: Command events record the wrapped tool's own exit when it ran

An `lstk_command` event SHALL carry `result.proxy_exit_code` if and only if the wrapped tool the user asked for ran to completion and the invocation's outcome is that tool's exit, and its value SHALL be that tool's own exit code, 0 included (or the platform's signal-termination value when a signal the tool did not trap killed it). The key SHALL be absent — not present with a null value — on lstk's own commands and on proxy invocations whose tool never ran, because the consumer reads presence as "the tool ran" and a null key would read as present.

A tool SHALL be recorded as having run only where the call site that ran the process declares the invocation to be the user's. lstk SHALL NOT infer that from the presence of a process-exit error in the chain, because lstk runs subprocesses of its own whose failures are indistinguishable from the user's by error type or message.

A wrapped tool that lstk never started — because lstk's own preflight, configuration, or resolution failed first, or because the tool could not be executed at all — SHALL record no `proxy_exit_code`.

#### Scenario: The user's tool rejects the user's command

- **WHEN** `lstk aws s3 lss` runs and the AWS CLI exits 252
- **THEN** the event has `result.proxy_exit_code` 252
- **AND** `result.exit_code` is 252, the AWS CLI's own code

#### Scenario: lstk's own subprocess fails

- **WHEN** `lstk setup azure` runs and its internal `az cloud list` call exits non-zero
- **THEN** the event has no `result.proxy_exit_code`
- **AND** `result.exit_code` is non-zero

#### Scenario: The tool is not installed

- **WHEN** `lstk aws s3 ls` runs and no `aws` executable is on PATH
- **THEN** the event has `parameters.proxied` true
- **AND** no `result.proxy_exit_code`

### Requirement: Command events record whether lstk was interrupted

Every `lstk_command` event SHALL carry `result.cancelled`, true when lstk's own signal context had been cancelled — the process received an interrupt or termination signal, or the interactive interface was quit — by the time the invocation finished, and false otherwise. It SHALL be emitted on every event, false included.

Cancellation SHALL NOT be determined from the wrapped tool's exit code. A wrapped tool that handles the signal and shuts down cleanly exits with an ordinary code rather than by signal, so an exit-code test would attribute the interruption to the tool; on Windows no exit code indicates signal termination at all.

The field is a raw observation: an invocation that completed successfully while a signal arrived records `cancelled` true and `exit_code` 0. Consumers SHALL treat a zero exit as a success regardless of `cancelled`.

#### Scenario: An interrupted proxy invocation records both the interruption and the tool's exit

- **WHEN** `lstk terraform apply` is interrupted and terraform traps the signal, cleans up, and exits 1
- **THEN** the event has `result.cancelled` true
- **AND** `result.proxy_exit_code` 1

#### Scenario: An interrupted lstk command records the interruption

- **WHEN** `lstk start` is interrupted from the keyboard before the emulator is ready
- **THEN** the event has `result.cancelled` true
- **AND** `parameters.proxied` false

#### Scenario: A command that finished is still a success

- **WHEN** a command returns successfully and a termination signal arrives as it finishes
- **THEN** the event has `result.exit_code` 0
- **AND** consumers count it as a success whatever `result.cancelled` says

### Requirement: The recorded exit code is unchanged by attribution

Adding the three fields SHALL NOT change `result.exit_code`. A wrapped tool's non-zero exit SHALL continue to be recorded as that tool's exact code, and the recorded value SHALL continue to equal the code the lstk process itself terminates with.

Consumers SHALL NOT infer a failure's origin from `exit_code`. lstk propagates a child's exit code in some of its own failures too, so a given code may appear on an invocation with or without a `proxy_exit_code`; the presence and value of `proxy_exit_code`, together with `proxied` and `cancelled`, are the only attribution.

#### Scenario: The tool's exit code survives attribution

- **WHEN** a wrapped tool exits with code 252
- **THEN** `result.exit_code` is 252 and `result.proxy_exit_code` is 252

#### Scenario: An lstk failure may still carry a child's exit code

- **WHEN** an lstk-composed subprocess fails and lstk propagates its exit code
- **THEN** `result.exit_code` is that subprocess's code
- **AND** the event has no `result.proxy_exit_code`

#### Scenario: The tool succeeds and lstk then fails

- **WHEN** the wrapped tool exits 0 and lstk fails afterwards
- **THEN** the event has no `result.proxy_exit_code`, since the failure is not the tool's exit
- **AND** `result.exit_code` is non-zero
