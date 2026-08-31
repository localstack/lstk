# Tasks

> Implemented following the proposed decision in `design.md` (Option B: full virtualization, with Option A's teardown hygiene). If the team decides for Option A instead, sections 1 and 2 are revised; the change is contained in `internal/proc`.

## 1. Core PTY input (internal/proc)

- [x] 1.1 Wire interactive stdin in `RunInPTY`: terminal-stdin detection, raw mode with restore, stdin→master pump, Setsid+Setctty (Option B)
- [x] 1.2 Adopt cancelreader-based pump teardown (cancel, join, close, surface restore errors) from the Option A prototype
- [x] 1.3 Degrade to output-only PTY when raw mode cannot be enabled (keep DEVX-1026 behavior)
- [x] 1.4 Platform split (`pty_unix.go` / `pty_windows.go`); Windows keeps the fall-back-to-`Run` contract

## 2. Tests

- [x] 2.1 `internal/proc` unit tests: keystroke forwarding via outer PTY, termios restored after exit, input pump torn down before return, redirected-stdin passthrough stays byte-exact and non-terminal
- [x] 2.2 faketool `Pager` mode (raw read from fd 2) from the Option A prototype
- [x] 2.3 Integration tests for `lstk aws` and `lstk az`: pager receives keystrokes, `q` exits; mock health server, no Docker
- [x] 2.4 Pin the accepted ^Z no-op (SIGTSTP discarded on the orphaned child group) in a test so it is documented, not rediscovered

## 3. Docs

- [x] 3.1 Update CLAUDE.md Signal Forwarding / PTY paragraphs (interactive-stdin path, redirected-stdin carve-out)
- [x] 3.2 Doc comments on `RunInPTY` and the input-wiring helper carrying the full rationale (less ≥577 ttyname(2), session/ctty reasoning)
