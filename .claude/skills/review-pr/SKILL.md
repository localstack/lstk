---
name: review-pr
description: Review a PR against lstk architectural patterns and coding conventions. Use when asked to review a pull request.
argument-hint: [pr-number]
allowed-tools: Read, Grep, Glob, Bash(gh pr diff *), Bash(gh pr view *), Bash(git diff *), Bash(git log *)
---

# Review PR

Review PR #$ARGUMENTS against lstk's architectural patterns and conventions.

## Step 1: Gather PR data

Run these commands to collect context:

```
gh pr diff $ARGUMENTS
gh pr view $ARGUMENTS --json title,body,files
```

## Step 2: Review checklist

Go through each changed file and check for violations. Flag only actual problems — don't nitpick style or formatting that's already consistent with the codebase.

### Architecture boundaries

- [ ] No business logic in `cmd/` — only Cobra wiring, output mode selection, and dependency creation
- [ ] Domain packages (`internal/container/`, `internal/auth/`, etc.) do not import `charmbracelet/bubbletea` or `internal/ui`
- [ ] Output mode (TUI vs plain) is selected at the command boundary in `cmd/`, not inside domain logic

### Output and event system

- [ ] No direct `fmt.Print`/`log.Print` in domain code — emits events via `sink.Emit(output.XxxEvent{...})` instead (no package-level emit helpers)
- [ ] New event types (if any) are added to all required locations:
  - `internal/output/events.go` (struct + `sealedEvent()` marker)
  - `internal/output/plain_format.go` (`FormatEventLine` case)
  - Tests in `internal/output/*_test.go`
- [ ] Event payloads carry domain facts, not pre-rendered UI strings
- [ ] `PlainSink` formatting has no lipgloss/styling imports

### Sink and dependency injection

- [ ] Domain functions accept `output.Sink` as a parameter (not constructed internally)
- [ ] No package-level global variables — dependencies injected via constructors

### User input

- [ ] Domain code never reads from stdin directly
- [ ] Interactive input uses `UserInputRequestEvent` + `ResponseCh` pattern
- [ ] Non-TTY mode fails early with a helpful error if input would be required
- [ ] New user-supplied inputs (args, flags, config values) are validated at the boundary via `internal/validate`; no new inline validation regexp duplicating it (identifiers → `ResourceName`; opaque secrets → loose checks like `AuthToken`; paths/URLs → their existing parsers) and malformed-input cases are tested

### TUI (if UI changes)

- [ ] `Update()` is non-blocking
- [ ] UI-only messages are suffixed with `...Msg`
- [ ] Styles use semantic names defined in `internal/ui/styles/styles.go`
- [ ] Components are single-concern and presentational only

### Testing

- [ ] New functionality has tests (prefer integration tests)
- [ ] Bug fixes have an integration test that reproduces the bug (fails before fix, passes after)
- [ ] Interactive tests use PTY (`github.com/creack/pty`)
- [ ] No unchecked errors outside of test files

### General

- [ ] User-facing text uses "emulator" (not "container" or "runtime")
- [ ] CLAUDE.md updated if architecture changed
- [ ] No unnecessary comments on self-explanatory code

### Review scope (self-merge vs. human review)

lstk pilots merging small PRs/bug fixes without a human approval; this checklist decides which bucket a PR falls into.

- [ ] New or changed user-facing behavior (new command, flag, output, or documented behavior) → needs human review
- [ ] Wasn't discussed beforehand (no prior Slack/Linear/design conversation), or is speculative/"nice to have" → needs human review, regardless of size
- [ ] Straightforward bug fix or internal refactor, no new user-facing behavior, small and self-contained diff → self-merge candidate
- [ ] Any doubt after the above → needs human review (default to the safer bucket)

## Step 3: Output

Provide a summary with:
1. **Verdict**: Approve / Request changes / Comment
2. **Issues found**: List each with file path, line, and why it's a problem
3. **Suggestions**: Optional improvements (clearly marked as non-blocking)
4. **Review recommendation**: whether a human review is advised or this looks like a self-merge candidate, with a one-line reason based on the Review scope checklist above

Keep feedback actionable and specific. Don't flag things that aren't problems.
