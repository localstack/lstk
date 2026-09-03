## ADDED Requirements

### Requirement: Keyboard input reaches a wrapped tool run on lstk's PTY

When lstk runs a wrapped tool on a pseudo-terminal (`proc.RunInPTY`) and lstk's own stdin is a terminal, keystrokes typed on the user's terminal SHALL be delivered to the wrapped tool through that PTY — including input read by the tool's descendants, such as the pager the AWS CLI spawns, regardless of whether that reader resolves its keyboard via the terminal attached to its stderr (`ttyname(2)`, less ≥ 577), `/dev/tty`, or file descriptor 2 directly. Individual keystrokes SHALL be delivered without requiring ENTER, and the user's terminal SHALL NOT echo them a second time.

The user's terminal state SHALL be restored when the wrapped tool exits, on both success and failure paths.

#### Scenario: Pager keystrokes control paged output (DEVX-1049)

- **WHEN** `lstk aws` runs on a terminal and the AWS CLI pages output taller than the window
- **THEN** SPACE advances a page, ENTER scrolls a line, and `q` exits the pager and the command
- **AND** after exit, the shell's terminal behaves normally (echo, line editing)

#### Scenario: The az proxy has the same behavior

- **WHEN** `lstk az` runs on a terminal and the Azure CLI pages output
- **THEN** the same keystrokes have the same effect

### Requirement: Redirected stdin bypasses the PTY

When lstk's stdin is not a terminal (piped or redirected), the wrapped tool SHALL receive that stdin as-is: bytes reach the tool unmodified by any terminal line discipline (no echo, no newline translation, no control-character interpretation), and the tool SHALL observe a non-terminal stdin. The PTY continues to serve the tool's stdout/stderr as before.

#### Scenario: Piped data is passed through byte-exact

- **WHEN** `lstk aws s3 cp - s3://bucket/key` runs with data piped to stdin while stdout/stderr are terminals
- **THEN** the aws CLI reads the piped bytes unmodified and sees a non-terminal stdin

### Requirement: Interrupting a wrapped interactive run

Ctrl-C typed during an interactive PTY run SHALL interrupt the wrapped tool exactly once, preserving the tool's opportunity to shut down gracefully (e.g. terraform releasing its state lock), and the run SHALL end with the tool's real exit status. The architecture decision in `design.md` governs the delivery path (real terminal's process group vs. the PTY's line discipline); either path MUST satisfy this requirement.

#### Scenario: Ctrl-C ends a streaming command

- **WHEN** the user presses Ctrl-C during `lstk aws logs tail --follow` on a terminal
- **THEN** the wrapped aws CLI terminates as it would when run directly, and lstk exits with the corresponding status
