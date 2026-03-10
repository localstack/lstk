---
name: add-event
description: Add a new output event type to the event/sink system. Use when adding a new kind of event for domain-to-UI communication.
argument-hint: <EventName> [description]
allowed-tools: Read, Write, Edit, Glob, Grep
---

# Add Output Event

Add a new event type `$ARGUMENTS` to the output event system.

## Reference: current codebase

Read these files first — they are the source of truth:

- `internal/output/events.go` — all event types, the `Event` union constraint, and emit helpers
- `internal/output/plain_format.go` — `FormatEventLine()` switch for plain text rendering
- `internal/output/plain_format_test.go` — test cases for format parity
- `internal/ui/app.go` — `Update()` method that handles events in the TUI

## Step 1: Define the event struct

In `internal/output/events.go`:

1. Add a new struct with fields that are **domain facts** (not pre-rendered strings):
   ```go
   type <Name>Event struct {
       // Fields should be typed data, not display strings
   }
   ```

2. Add the new type to the `Event` union constraint:
   ```go
   type Event interface {
       MessageEvent | AuthEvent | ... | <Name>Event
   }
   ```

3. Add an emit helper function:
   ```go
   func Emit<Name>(sink Sink, ...) {
       Emit(sink, <Name>Event{...})
   }
   ```

## Step 2: Add plain text formatting

In `internal/output/plain_format.go`, add a case to `FormatEventLine()`:

```go
case <Name>Event:
    return format<Name>(e), true
```

Write a `format<Name>()` helper that returns a single-line string. Keep formatting consistent with existing events — plain text, no ANSI codes, no lipgloss.

## Step 3: Add format tests

In `internal/output/plain_format_test.go`, add test cases to `TestFormatEventLine` covering:

- The happy path with all fields populated
- Edge cases (empty optional fields, zero values)
- The exact expected output string

Follow the existing table-driven test pattern with `name`, `event`, `want`, `wantOK` fields.

## Step 4: Handle in TUI

In `internal/ui/app.go`, add a case in `Update()` for the new event.

Decide based on the event's nature:
- **Simple text display**: Convert to a `styledLine` and append to `a.lines` (like `MessageEvent`)
- **Component update**: Update the relevant component (like `ErrorEvent` → `a.errorDisplay.Show()`)
- **Needs buffering**: If it should wait for spinner, check `a.spinner.PendingStop()` and buffer accordingly (like `MessageEvent`)

If the event doesn't need special TUI handling, the `default` case in `Update()` already falls back to `FormatEventLine()` — so you may not need to add anything here.

## Anti-patterns to avoid

- Do NOT put pre-rendered UI strings in event fields — use typed domain data
- Do NOT add lipgloss/styling imports to `plain_format.go`
- Do NOT skip the format test — every event type needs parity coverage
- Do NOT forget to add the type to the `Event` union — it won't compile without it
