## Why

`lstk aws` and `lstk az` run their wrapped tool on a pseudo-terminal (`proc.RunInPTY`, DEVX-1026/DEVX-1028) that is output-only: when the AWS CLI pages long output, the pager draws its first page and then ignores every keystroke — the command looks frozen (DEVX-1049; mechanism and the two candidate fixes in `design.md`). The same wrapper is intended to grow to the other wrapped tools (`terraform`, `cdk`, `sam`, extensions), all interactive to some degree, so the input side of the PTY needs a deliberate design, not a spot fix.

## What Changes

- **`proc.RunInPTY` gains interactive input**: when lstk's stdin is a real terminal, keystrokes are delivered to the wrapped tool through the inner PTY; redirected stdin (`yes | lstk aws ...`) keeps today's untouched passthrough.
- **One of two architectures** (the subject of `design.md`, decision pending team discussion): an *input bridge* that relays keystrokes while the real terminal keeps signal duty, or *full terminal virtualization* where the inner PTY becomes the child's complete controlling terminal.
- **Tests**: integration coverage for pager keystrokes (SPACE/ENTER/q) on both `aws` and `az`, plus guards for terminal-state restoration and redirected-stdin passthrough.

Two working prototypes exist, one per architecture; `design.md` contrasts them and proposes full virtualization. `tasks.md` is implemented against that proposed option and will be revised if the team decides otherwise.

## Capabilities

### Added Capabilities

- `wrapped-tool-pty`: when lstk's stdin is a terminal, keystrokes reach a PTY-wrapped tool (including grandchildren such as pagers), and the terminal state is restored on exit. Redirected stdin stays byte-exact and non-terminal. Ctrl-C interrupts the tool exactly once, preserving graceful shutdown.

## Impact

- **Touched code**: `internal/proc` (PTY wiring; possibly per-GOOS files), `go.mod` (option A adds `muesli/cancelreader`), CLAUDE.md's Signal Forwarding section.
- **Behavioral surface**: Ctrl-C/Ctrl-Z/job-control semantics of `lstk aws` / `lstk az` interactive runs differ per option — see the behavior matrix in `design.md`.
- **Tests**: `internal/proc/pty_test.go`, new pager integration tests, possibly a `faketool` pager mode.
- **Consumers**: no API change for `awscli.Exec` / `azurecli.Exec`; future PTY adoption by `terraform`/`cdk`/`sam` inherits whichever semantics are chosen here.

Closes DEVX-1049.
