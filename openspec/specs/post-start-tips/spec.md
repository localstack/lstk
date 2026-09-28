# post-start-tips Specification

## Purpose

Defines which single `> Tip:` line, if any, lstk shows after starting an emulator, including tips that are shown only when a detector finds something worth suggesting — the first being the tip that points users at `lstk deploy` when the current directory contains infrastructure as code.

## Requirements

### Requirement: At most one post-start tip per run
Whenever an emulator start completes successfully (`lstk start`, the bare `lstk` root, `lstk restart`, or a snapshot load's auto-start), whether the emulator was started by this run or was already running, the system SHALL print at most one line beginning with `> Tip:`. No detector outcome (found, not found, error, timeout) SHALL cause a second tip line, and a detector that declines SHALL NOT leave the run without a tip when a static tip is eligible.

#### Scenario: Detector tip replaces, not adds to, the rotating tip
- **WHEN** an AWS emulator start shows the deploy tip
- **THEN** the output contains exactly one `> Tip:` line, and it is the deploy tip

### Requirement: Tip priority tiers
Tips SHALL be grouped into priority tiers, and a tip from a lower tier SHALL be shown only when no tip in a higher tier is eligible. The first-run shell-completion tip SHALL be the highest tier. It is eligible only for an interactive `lstk` or `lstk start` that began with no config file and without `--config` (`--type` does not change this). `lstk restart` and a snapshot load's auto-start are never first runs. The per-emulator rotating tips, detector tips included, SHALL be the next tier, and within that tier the candidate SHALL be chosen at random with equal weight among eligible tips.

#### Scenario: First run keeps the completion tip
- **WHEN** an interactive first run starts an AWS emulator in a directory containing Terraform files
- **THEN** the only tip shown is the shell-completion tip and no detector is run

#### Scenario: Restart goes straight to the rotating tier
- **WHEN** `lstk restart` completes for an AWS emulator
- **THEN** the shell-completion tip is not shown, and the tip is chosen from the AWS rotating tips, the deploy tip included when eligible

### Requirement: Detector tips
A tip MAY depend on a detector: a check run at tip-selection time whose result decides whether the tip is shown and may supply part of its text. A detector tip SHALL be shown only when its detector reports something useful. The system SHALL run a detector only after random selection has chosen its tip, SHALL run at most one detector per run, and SHALL NOT run a detector for a tip that was not eligible. When the chosen detector reports nothing useful, fails, or exceeds its deadline, the system SHALL show a tip from the same tier that needs no detector, chosen at random, and SHALL NOT run any further detector in that run.

#### Scenario: Static tip chosen
- **WHEN** random selection chooses a tip that has no detector
- **THEN** no detector runs and that tip is shown

#### Scenario: Detector tip chosen and detector reports something
- **WHEN** random selection chooses a detector tip and its detector reports a useful result
- **THEN** that detector ran exactly once and its tip is shown

#### Scenario: Detector declines
- **WHEN** random selection chooses a detector tip and its detector reports nothing useful
- **THEN** a tip without a detector from the same tier is shown, and no other detector runs in that run

### Requirement: Detectors are invisible and cannot harm startup
A detector MAY do any work needed to decide its tip, for example running an lstk extension or another program, querying the emulator or an API, or reading the working directory. Whatever it does, the system SHALL guarantee the following, even for a detector that misbehaves:
- no detector output reaches the user;
- the start waits no longer than a short deadline for the detector, even if the detector ignores cancellation;
- a detector that fails, panics, produces unusable results, or overruns the deadline is treated as having nothing useful to report, and SHALL NOT fail the start, change its exit code, or print an error.

A detector SHALL have no user-visible side effects. It SHALL NOT prompt for input, write configuration, start or stop an emulator, or cause telemetry that reports the lookup as a user action. A program it runs SHALL be told not to emit telemetry, and an lstk extension SHALL receive `LOCALSTACK_DISABLE_EVENTS=1`.

#### Scenario: Detector fails or hangs
- **WHEN** the chosen detector returns an error or does not finish within the deadline
- **THEN** the start succeeds with its usual exit code, no detector output or error is printed, and a tip without a detector is shown

#### Scenario: Detector ignores the deadline
- **WHEN** the chosen detector keeps running past the deadline without honouring cancellation
- **THEN** the start completes once the deadline passes, and a tip without a detector is shown

#### Scenario: Detector panics
- **WHEN** the chosen detector panics
- **THEN** the start succeeds with its usual exit code, and a tip without a detector is shown

#### Scenario: Extension-backed detector telemetry suppressed
- **WHEN** a detector runs an lstk extension
- **THEN** the extension's environment disables LocalStack event reporting (`LOCALSTACK_DISABLE_EVENTS=1`)

### Requirement: Deploy tip
The AWS rotating tips SHALL include a deploy tip whose detector runs `lstk deploy detect` in the working directory lstk was started in, in machine-readable mode, and reads the IaC tools it reports. The deploy tip SHALL be eligible only when the emulator is AWS and a `deploy` extension resolves by the same rules `lstk deploy` uses; whether the emulator was already running SHALL NOT affect eligibility. The detector SHALL report something useful only when at least one tool is detected.

The deploy tip SHALL read `> Tip: Deploy your <Tool> project to LocalStack: lstk deploy` when exactly one known tool is detected, where `<Tool>` is `Terraform` (for `terraform`), `CDK` (for `cdk`), or `SAM` (for `sam`). Otherwise (more than one tool, or one unrecognized tool) it SHALL read `> Tip: Deploy your infrastructure as code to LocalStack: lstk deploy`.

#### Scenario: Snowflake or Azure start never offers the deploy tip
- **WHEN** a Snowflake or Azure emulator is started in a directory containing Terraform files
- **THEN** the deploy tip is not shown and `lstk deploy detect` is not run

#### Scenario: Already-running AWS emulator can show the deploy tip
- **WHEN** `lstk start` finds the AWS emulator already running, in a directory containing Terraform files, and random selection chooses the deploy tip
- **THEN** `lstk deploy detect` runs once and the tip line is `> Tip: Deploy your Terraform project to LocalStack: lstk deploy`

#### Scenario: No deploy extension installed
- **WHEN** an AWS emulator start completes and no `deploy` extension resolves
- **THEN** the deploy tip is never a candidate and the rotating tip is chosen from the AWS tips that existed before this change

#### Scenario: No IaC in the directory
- **WHEN** the deploy tip is chosen and `lstk deploy detect` reports no tools
- **THEN** the run shows one of the other AWS rotating tips and exits 0

#### Scenario: Multiple tools detected
- **WHEN** the deploy tip is chosen and detection reports `terraform` and `cdk`
- **THEN** the tip line is `> Tip: Deploy your infrastructure as code to LocalStack: lstk deploy`
