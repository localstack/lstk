## Why

`lstk logs --follow` renders log lines narrower than the terminal because `HardWrap` counts ANSI escape code characters as visible width. Styled prefixes (e.g. `styles.Secondary.Render("container | ")`) embed invisible escape sequences that are included in the rune count, so wrapping fires before the visible content reaches the terminal edge.

## What Changes

- Log lines are wrapped using ANSI-aware width calculation so they fill the full terminal width.
- The `renderLogLine` rendering path in `app.go` is updated to measure visible prefix width and wrap the message content independently before re-combining with the styled prefix.

## Capabilities

### New Capabilities

- `logs-full-width-rendering`: Log lines in `--follow` mode wrap at the correct terminal width by accounting for invisible ANSI sequences in styled prefixes.

### Modified Capabilities

## Impact

- `internal/ui/logrender.go` — `renderLogLine` signature or callers updated to accept a `width` parameter
- `internal/ui/app.go` — `output.LogLineEvent` handler uses ANSI-aware wrapping
- `internal/ui/wrap/` — may need a new ANSI-aware wrap helper
