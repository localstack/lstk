---
name: add-command
description: Scaffold a new CLI subcommand following lstk patterns. Use when adding a new command to the CLI.
argument-hint: <command-name>
allowed-tools: Read, Write, Edit, Glob, Grep
---

# Add CLI Command

Scaffold a new CLI subcommand named `$ARGUMENTS` following lstk's architecture.

## Step 0: Clarify requirements and challenge the design

Before writing any code, understand what the command should do and whether the approach is sound. Ask the user these questions, plus any others that make sense given the specific command. Wait for answers before proceeding.

**Core questions:**
1. **What does this command do?** (one sentence — e.g., "shows the status of running emulators")
2. **Does it need to talk to Docker/the runtime?** (determines whether `runtime.Runtime` is a dependency)
3. **Does it need configuration?** (determines whether `PreRunE: initConfig` is needed)
4. **Does it need authentication?** (determines whether auth flow is involved)
5. **Does it need any new event types?** (e.g., a new kind of progress, a new status phase — if yes, use `/add-event` for each)

**Also ask context-specific questions** that aren't in this list but would help clarify behavior — e.g., edge cases ("what happens if no emulators are running?"), flags/arguments the command should accept, expected output format, whether it should be idempotent, etc.

**Challenge the architecture** if something doesn't fit well:
- If the command overlaps with an existing one, ask whether it should be a subcommand or flag instead
- If the proposed behavior mixes concerns (e.g., fetching data AND mutating state), suggest splitting it
- If a simpler approach exists (reusing existing domain functions, adding a flag to an existing command), propose it

Skip questions where the answer is obvious from context (e.g., if the user already explained the behavior in detail). Be direct — raise concerns early rather than building something that needs reworking.

## Reference: current codebase

Read these files before writing anything — they are the source of truth for patterns:

- `cmd/stop.go` — simplest command pattern (cmd wiring + output mode selection)
- `cmd/root.go` — where commands are registered via `AddCommand()`
- `internal/container/stop.go` — simplest domain logic pattern (accepts `output.Sink`, emits events)

## Step 1: Create the command file in `cmd/`

Create `cmd/$ARGUMENTS.go` with:

- A `new<Name>Cmd()` factory function returning `*cobra.Command`
- `PreRunE: initConfig` if the command needs configuration
- Output mode decision at the boundary:
  - Interactive: delegate to `ui.Run<Name>(...)` or TUI path
  - Non-interactive: call domain function with `output.NewPlainSink(os.Stdout)`
- No business logic — only Cobra wiring, dependency creation, and output mode selection

## Step 2: Create the domain logic package

Create `internal/<package>/<name>.go` (use an existing package if it fits, or create a new one) with:

- A function that accepts `ctx context.Context`, `rt runtime.Runtime`, `sink output.Sink`, and any other dependencies
- Emit events via `output.EmitXxx(sink, ...)` — never `fmt.Print` or `log.Print`
- Return errors normally; use `output.NewSilentError(err)` only if the error was already displayed via `EmitError`
- No imports from `internal/ui` or `charmbracelet/bubbletea`

## Step 3: Register the command

In `cmd/root.go`, add the new command to `root.AddCommand(...)`.

If the command constructor needs dependencies (like `*env.Env` or `*telemetry.Client`), add them as parameters matching the existing pattern.

## Step 4: Add new event types (if needed)

If the command needs to communicate domain state that doesn't fit existing event types, use `/add-event` for each new event. Common cases:

- New status phases → may just need a new string in `ContainerStatusEvent.Phase`
- New kinds of progress → may need a new event type
- New structured errors → use `ErrorEvent` with appropriate `Actions`

Prefer reusing existing events before creating new ones.

## Step 5: Add TUI path (if needed)

If the command has interactive output beyond plain text:

1. Add a `Run<Name>()` function in `internal/ui/` following the pattern in `internal/ui/run.go`
2. Create the Bubble Tea program, run domain logic in a goroutine, send events via `output.NewTUISink(programSender{p: p})`

If the command needs custom UI elements (progress bars, tables, etc.), use `/add-component` for each new component.

If the command is simple (just spinner + success/error messages), the existing `App` model handles those events already — you may not need a custom TUI path.

## Step 6: Add integration test

Create `test/integration/<name>_test.go` with:

- Non-interactive tests: `exec.CommandContext(ctx, binaryPath(), "<name>")` → `cmd.CombinedOutput()`
- Interactive (TUI) tests: use `pty.Start(cmd)` from `github.com/creack/pty`
- Use `requireDocker(t)` if Docker is needed
- Use `cleanup()` and `t.Cleanup(cleanup)` for container state
- Use `context.WithTimeout` for all tests

## Telemetry

Every new command must emit an `lstk_command` telemetry event. Wrap the command's `RunE` with `commandWithTelemetry(name, tel, func() string { return cfg.AuthToken }, fn)` — this handles timing, exit code, and error message automatically. The token resolver is called after the command runs, allowing commands like `login` to resolve the post-execution token.

Start and stop are exceptions: they emit `lstk_lifecycle` events in addition to `lstk_command`, so they manage their own telemetry manually instead of using `commandWithTelemetry`.

In the corresponding integration test, add an assertion that the `lstk_command` event was emitted.

## Anti-patterns to avoid

- Do NOT put business logic in `cmd/` — the command file should be thin wiring only
- Do NOT construct sinks inside domain code — always accept `output.Sink` as a parameter
- Do NOT use `fmt.Print`/`log.Print` in domain code — use `output.EmitXxx()` helpers
- Do NOT import `internal/ui` or Bubble Tea from domain packages
- Do NOT create package-level global variables — inject dependencies via constructors
- Do NOT use "container" or "runtime" in user-facing text — use "emulator"
