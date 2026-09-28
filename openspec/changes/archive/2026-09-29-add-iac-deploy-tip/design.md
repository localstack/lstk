# Design

## Context

- Post-start tips are decided in one place, `selectTip` in `internal/container/tips.go`, and emitted from one place, `emitPostStartTip`, which is called only from `container.Start` after a successful start. Today `selectTip` returns one string: the completion tip if `firstRun && interactive`, otherwise a `rand.IntN` pick from a per-emulator list of fixed strings.
- `Start` runs for `lstk`/`lstk start` (interactive via `ui.Run`, non-interactive directly), `lstk restart` (`ui.RunRestart`), and snapshot-load auto-start. It also runs when the emulator was already running: `emitAlreadyRunning` returns a typed `StartResult`, so rotating tips show there too.
- `StartOptions.FirstRun` is true only when `initConfigDeferCreate` → `config.Load` found no `config.toml` for `lstk`/`lstk start`. It is never true with `--config <path>`, and restart and snapshot auto-start hard-code `false`. `--type` on a fresh install keeps it true (`wasFirstRun`).
- `lstk deploy` is a bundled extension (multi-call binary, dispatched on `argv[0] = lstk-deploy`). `deploy detect` scans the working directory to depth 3. With `LSTK_EXT_CONTEXT.json = true` it writes one envelope to stdout whose `data.tools` is always a string array (`terraform`, `cdk`, `sam`). Zero tools is still `status: "ok"` with exit 0. It refuses to run without `authToken` in the context, and it emits its own telemetry unless `LOCALSTACK_DISABLE_EVENTS=1`.
- lstk resolves extensions with `extension.NewResolver(logger).Resolve(name)` and builds the child env with `extension.Context.Environ`. The only exec path today, `extension.Invoke`, wires the user's terminal straight through, so it is unsuitable for a captured probe.
- Domain packages must not call `config.Get()`, and they take their dependencies explicitly (no package-level globals).

## Goals / Non-Goals

**Goals:**
- A detector can do anything (run an extension or another program, call an API, call a domain function in-process). Adding a detector tip means one constructor in the `tips` package and one line in `buildStartOptions`, with no new `StartOptions` field.
- `tips.Select` is the single decision point, and `Start` remains the single emit site.
- The selector enforces the startup guarantees (deadline, panic safety, silence) itself, so they hold even for a detector that misbehaves.
- No subprocess on the common path, and at most one detector (so one deadline) per run.

**Non-Goals:**
- Caching detector results across runs.
- Showing detect's support analysis (unsupported-resource counts) in the tip.
- Weighting or prioritizing tips within a tier.
- A test-only env var to force the pick.
- Changing how `FirstRun` is resolved.

## Decisions

### 1. A tip is a value with eligibility and an optional detector
```go
type Tip struct {
    text     string                                    // static tips
    eligible func(config.EmulatorType) bool            // cheap, no I/O; nil = always
    detect   func(ctx context.Context) (string, bool)  // nil = static
}
```
A detector gets everything it needs from its constructor (decision 3), so `detect` takes only `ctx`. Its inputs are then visible in the constructor's signature, with no second path. The emulator type is the only post-start fact any rotating tip judges by, so `eligible` takes just that. `eligible` must be side-effect free because it runs for every candidate, and only `detect` may do I/O.

*Alternative:* a `tipFacts` struct (emulator type, first run, interactive, at one point the auth token) passed to both functions, so a new fact wouldn't widen every tip's signature. It was removed because no detector read it once the token moved into the constructor, and two of its three fields only served the completion tip. If a future tip needs another fact, widening `eligible` (or bringing back a struct) is a small, local change.

`Tip`'s fields stay unexported, so a `Tip` can only be built inside the `tips` package. That keeps every tip's text and rules in that one package.

*Alternative:* a `Tip` interface with `Static`/`Detector` implementations. That is more ceremony for the same thing, and a struct literal list reads like today's string list.

### 2. Selection: tiers, uniform pick, one-detector budget
```
if firstRun && interactive: return completionTip   // highest tier: no pick, no detector
candidates = eligible(type) tips in rotatingTips(type) + opts.DetectorTips
if none: return ""
t = candidates[intn(len)]
if t is static: return t.text
text, ok = runDetector(t)                          // the run's only detector
if ok: return text
statics = static candidates
if statics: return statics[intn(len)]
return ""
```
- The pick is uniform over *eligible* candidates, so an ineligible deploy tip (non-AWS, no extension) leaves the existing tips' odds exactly as they are today.
- The fallback draws only from static tips, which is what enforces "one detector per run" even once a tier holds several detector tips.
- The completion tier is the explicit first-run check that existed before this change, not a tier entry. It is the only tip in its tier, so modelling it as a pickable candidate added nothing.
- `intn` is injected (defaulting to `rand.IntN`) so unit tests pick deterministically.
- Detector tips from `StartOptions.DetectorTips` join the rotating tier. Only the rotating tier holds detector tips today, and a second tier can be added when a real tip needs one.

*Alternative:* shuffle and try candidates until one resolves. That was rejected because it can run several detectors and stack their deadlines.

### 3. Detectors are opaque functions built with explicit dependencies, in a `tips` package
Tips live in their own package, `internal/container/tips`:
```
internal/container/tips/
  tips.go             Tip, staticTip, completion + rotating static tips, Select
  detectors.go        detectorDeadline, runDetector, the detector contract
  deploy_detector.go  NewDeployTip, deployToolName
```
Detector constructors take their dependencies as parameters, for example `NewDeployTip(resolver *extension.Resolver, configDir, authToken string) Tip`. `tips` imports what its detectors need (`internal/extension` today, perhaps an API client later), so `container` doesn't. `tips` never imports `output`. `container.Start` calls `tips.Select` and emits the result itself, so it stays the only emit site.

The package is nested under `container`, not placed at `internal/tips`, because tips are purely a post-start concern and only `container.Start` may emit one. A top-level package would suggest other commands show tips too. This follows the existing nesting pattern of `internal/emulator/aws` and `internal/iac/terraform`.

The command boundary assembles the list, because only it may resolve config and build resolvers or clients:
```go
// cmd/root.go buildStartOptions
DetectorTips: []tips.Tip{
    tips.NewDeployTip(extension.NewResolver(logger), configDir, cfg.AuthToken),
},
```
Every `Start` caller (start, root, restart, snapshot auto-start) goes through `buildStartOptions`, so each gets the same list. A nil or empty list means static tips only, and tests use that by default.

How a detector does its work is up to it. Guidance for future detectors:
- **Built-in lstk behaviour:** call the domain function in-process. Don't re-exec `lstk <cmd> --json`. That is faster and typed, and it avoids telemetry, config reloads, and re-entering tip selection. Running `lstk start`/`stop` from a detector is forbidden by the spec.
- **Extensions:** use `extension.InvokeQuietlyWithJSON` (decision 4).
- **Other programs or APIs:** honour `ctx`, capture all output, and suppress telemetry. Calls to the LocalStack platform with the user's token count as a side effect and need the same scrutiny.

*Alternative (option B):* a single capability interface in `StartOptions` (e.g. `ExtensionRunner`) that detectors call through. It was rejected because every new kind of detector work would add a method, making it a service locator. Opaque detectors keep what a detector may do out of the selector's interface.

*Alternative:* keep everything in `internal/container/tips.go`. This was the first choice. It was reversed once the file reached about 220 lines across three concerns (static tips and selection, detector enforcement, the deploy detector), with `container` importing detector dependencies and hosting a stub `TestMain` for the deploy tests.

### 4. `extension.InvokeQuietlyWithJSON`: a plain helper that owns its guarantees
```go
func InvokeQuietlyWithJSON(ctx context.Context, ext *Extension, args []string, opts QuietOptions) (json.RawMessage, error)

type QuietOptions struct { // only what a caller legitimately chooses
    ConfigDir string
    AuthToken string
}
```
It is the counterpart to `Invoke`. `Invoke` runs an extension on the user's behalf, wired to their terminal. `InvokeQuietlyWithJSON` runs one on lstk's own behalf, and the user never sees the run. The helper itself enforces everything that implies, rather than leaving it to callers:
- the extension context is always `JSON: true` and `NonInteractive: true`, with no `SessionID`/`MachineID`/`EndpointURL` and empty `Emulators`;
- the environment is that context's `Environ(os.Environ())` plus `LOCALSTACK_DISABLE_EVENTS=1`;
- `exec.CommandContext` is used with `Args[0] = ext.Argv0`, nil stdin, stdout captured, stderr discarded, and `WaitDelay` set (for the same grandchild-pipe reason documented for `aws_completer`);
- the envelope is decoded, and `data` is returned when `status == "ok"`, an error otherwise.

The name covers the visible part ("Quietly") and the output format ("WithJSON"), so the doc comment must list the rest (no telemetry, never interactive). A new field in `QuietOptions` should come with a justification in that comment.

It does not go through `proc.Run`/`Invoke`, because this is a short captured-output exec, which CLAUDE.md already exempts from signal forwarding. The working directory is inherited, and that is what makes `deploy detect` scan the user's directory.

*Alternatives:* `RunJSON`, rejected because it names the output format and none of the guarantees. `Query`/`Probe` were also considered, as was plain `InvokeQuietly`, which hides that the result is the JSON envelope's `data`. A caller-supplied full `extension.Context` was rejected because a caller could turn JSON mode off or leak a session id, breaking what the name promises.

### 5. The selector enforces the guarantees
`runDetector` runs `detect` in a goroutine under `context.WithTimeout(ctx, detectorDeadline)` (a named const, 2s). It then `select`s on a result channel and `ctx.Done()`:
- **Deadline:** when the deadline expires, the result is discarded and treated as declined, even if the detector ignores `ctx`. The channel is buffered, so a late detector's send never blocks, and its goroutine ends on its own. A subprocess started with `exec.CommandContext` is killed by the cancelled context.
- **Panic:** a `recover()` in the goroutine turns a panic into a declined result, and logs it via `log.Logger`.
- **Silence:** detectors get no `output.Sink`. Anything they run must have its output captured. That is the detector's own contract and is covered by its tests.

### 6. The deploy tip
- `NewDeployTip(resolver, configDir, authToken string)`.
- Token: exactly what `lstk <ext>` dispatch passes, which is `cfg.AuthToken` (`cmd/root.go` resolves it at startup from `LOCALSTACK_AUTH_TOKEN`, then the keyring, trimmed and validated). `buildDetectorTips` hands it to the constructor. A token obtained by a browser login during the start is not seen. In that case `deploy detect` refuses, which is a declined detector, so a static tip shows.
  *Alternatives:* taking the token `start()` resolved, via an unexported `StartResult` field. It was rejected because it pushed tip plumbing into a public result type and created a second token path next to dispatch's. Re-running root's resolution when detect runs was also rejected: root overwrites `cfg.AuthToken` with its result, so a second pass treats the startup token as the env value and still returns it (a re-login after a license rejection would get the rejected token). Truly re-deriving would need the raw env value kept separately, which is more plumbing than the rare case is worth.
- `eligible`: the emulator type is AWS and `resolver.Resolve("deploy")` succeeds. That is a cheap filesystem lookup with no exec. A broken bundle counts as not eligible. Whether the emulator was already running is ignored, and the first run never reaches the rotating tier when the completion tip applies.
- `detect`: resolve again (to get the `*Extension`), then `InvokeQuietlyWithJSON(ctx, ext, ["detect"], QuietOptions{ConfigDir: configDir, AuthToken: authToken})`. It decodes `data.tools`. Empty or undecodable means `ok = false`.
- Text: exactly one of `terraform`/`cdk`/`sam` becomes `Terraform`/`CDK`/`SAM` in `Deploy your <Tool> project to LocalStack: lstk deploy`. Otherwise it is `Deploy your infrastructure as code to LocalStack: lstk deploy`, which also covers a single unknown value, so the tip keeps working if the extension adds a new tool.

## Risks / Trade-offs

- [The chosen detector adds up to 2s to the final line of a start in a huge directory] → the selector enforces the deadline, and only about 1/3 of AWS starts (with two static tips) ever run a detector.
- [A detector that ignores `ctx` leaves a goroutine running past the deadline] → the start doesn't wait for it, and lstk exits shortly after the tip anyway. A detector that runs subprocesses must use `exec.CommandContext`, so nothing outlives the process.
- [The `tips` package accumulates imports for detector dependencies] → accepted. They are confined to `tips`, and each detector's dependencies are visible in its constructor.
- [Contract drift in `deploy detect`'s envelope] → any decode failure is a declined detector and falls back to a static tip. The integration fake mirrors the documented shape.
- [A `PATH`-installed third-party `lstk-deploy` is exec'd with the auth token] → this is the same trust decision `lstk deploy` already makes, with identical resolution rules, and bundled wins over `PATH`.
- [The random pick makes the e2e test non-deterministic] → the test starts the emulator once, then re-runs `lstk start` against the already-running emulator (fast, no container start) until the deploy tip appears, with a bounded loop (e.g. 20 runs, miss probability about (2/3)^20 ≈ 0.03%). Every iteration also asserts the invariants: exactly one tip, and "no detect recorded unless the deploy tip was shown". Unit tests with an injected `intn` cover each branch deterministically.

## Migration Plan

Additive, with no config or flag changes. Older bundles without `deploy` never make the deploy tip eligible. Rollback is a revert.
