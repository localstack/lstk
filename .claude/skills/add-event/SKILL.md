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

- `internal/output/events.go` — all event types, the `Event` marker interface, and its `sealedEvent()` implementations
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

2. Add the marker method so the type satisfies the `Event` interface and `Sink.Emit` accepts it:
   ```go
   func (<Name>Event) sealedEvent() {}
   ```

   Call sites emit directly on the sink — no helper needed:
   ```go
   sink.Emit(output.<Name>Event{...})
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
- Do NOT add a package-level emit helper — call sites use `sink.Emit(output.<Name>Event{...})` directly
- Do NOT forget to add `func (<Name>Event) sealedEvent() {}` — without it `Sink.Emit` will reject the type at compile time
