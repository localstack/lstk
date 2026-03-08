---
name: add-component
description: Scaffold a new Bubble Tea TUI component following lstk's UI patterns. Use when adding a new reusable UI element.
argument-hint: <component-name>
allowed-tools: Read, Write, Edit, Glob, Grep
---

# Add TUI Component

Scaffold a new Bubble Tea component named `$ARGUMENTS`.

## Reference: current codebase

Read these files first — they are the source of truth:

- `internal/ui/components/spinner.go` — stateful component with Update/View pattern
- `internal/ui/components/error_display.go` — simple show/hide component driven by events
- `internal/ui/components/header.go` — pure presentational component (View only)
- `internal/ui/styles/styles.go` — all style definitions
- `internal/ui/app.go` — how components are composed in the main App model

## Step 1: Create the component

Create `internal/ui/components/<name>.go` with:

1. **A struct** holding component state:
   ```go
   type <Name> struct {
       // Private fields for internal state
       visible bool
   }
   ```

2. **A constructor**:
   ```go
   func New<Name>() <Name> {
       return <Name>{}
   }
   ```

3. **State mutation methods** that return a new copy (value receiver pattern, like Spinner):
   ```go
   func (c <Name>) Show(...) <Name> {
       c.visible = true
       // set fields
       return c
   }
   ```

4. **A View method**:
   ```go
   func (c <Name>) View() string {
       if !c.visible {
           return ""
       }
       // Render using styles from internal/ui/styles
   }
   ```

5. **An Update method** (only if the component handles tea.Msg internally, like Spinner):
   ```go
   func (c <Name>) Update(msg tea.Msg) (<Name>, tea.Cmd) {
       // Must be non-blocking
   }
   ```

## Step 2: Add styles (if needed)

In `internal/ui/styles/styles.go`, add new style variables with **semantic names**:

```go
var (
    <Name>Style = lipgloss.NewStyle().
        Foreground(lipgloss.Color("..."))
)
```

Use existing palette constants (`NimboDarkColor`, `NimboMidColor`, `NimboLightColor`) or standard ANSI color codes. Name styles after what they represent, not how they look.

## Step 3: Wire into App

In `internal/ui/app.go`:

1. Add the component as a field on the `App` struct
2. Initialize it in `NewApp()`
3. Handle relevant events in `Update()` to drive the component
4. Render it in `View()` at the appropriate position

## Step 4: Add component test

Create `internal/ui/components/<name>_test.go` with tests covering:

- Initial state (hidden/empty)
- State transitions (Show/Hide)
- View output for different states
- Edge cases (empty data, zero values)

Follow the existing test patterns in `spinner_test.go` or `error_display_test.go`.

## Step 5: Add event type (if needed)

If the component is driven by a new domain event, use `/add-event` to create the event type first. The component should consume the event — not define it. Events live in `internal/output/`, not in `internal/ui/components/`.

## UI-only messages

If the component needs messages that are purely UI concerns (not domain events), define them in the component file with the `...Msg` suffix:

```go
type <Name>DoneMsg struct{}
```

These are for internal component coordination only and should not appear in `internal/output/`.

## Anti-patterns to avoid

- Do NOT put business logic in components — they are presentational only
- Do NOT make `Update()` blocking — no network calls, no file I/O, no channel waits
- Do NOT define domain events in component files — events belong in `internal/output/events.go`
- Do NOT import domain packages from components (except `internal/output` for event types and `internal/ui/styles` for styling)
- Do NOT create "god components" — keep each component focused on a single concern
