# Proposal

## Why

`lstk deploy` (a bundled extension) can deploy a workspace's Terraform, CDK, or SAM/CloudFormation code straight into LocalStack, but nothing points users at it. Right after `lstk start` in a directory that contains IaC is exactly when that suggestion is useful, and the post-start rotating tip is the established place for it.

Today every tip is a fixed string. A tip like this one is only worth showing when a check finds something, and more such tips are likely, so the tip mechanism should support "show this only if its detector reports something useful" in general rather than special-casing deploy.

## What Changes

- Generalize post-start tips: a tip is either static or backed by a **detector** that runs at selection time and decides whether the tip is shown (and may fill in its text).
- Make tip priority explicit as tiers: the first-run completion tip, then the per-emulator rotating tips (static and detector tips together, picked uniformly at random).
- A detector runs **only when random selection chose its tip**, and **at most one detector runs per run**. If it reports nothing useful, fails, or exceeds its deadline, a static tip from the same tier is shown instead — never a second detector.
- Detectors are invisible: bounded by a deadline, no output reaches the user, never affect the start's success or exit code, and have no user-visible side effects (no prompts, config writes, emulator start/stop, or telemetry reporting the lookup as user activity).
- A detector may do any work (run an extension or another program, call an API, call lstk domain code in-process). Detectors are built at the command boundary with explicit dependencies and handed to the start as one generic list, so a future detector tip needs no new `StartOptions` field. The selector itself enforces the deadline and panic safety, so a misbehaving detector still can't harm the start.
- Add the first detector tip, for AWS: it runs `lstk deploy detect` in the current directory and, when IaC tools are found, suggests `lstk deploy`. It is eligible whenever the emulator is AWS and a `deploy` extension resolves, whether or not the emulator was already running.
- No new flags, config keys, or output events. The "at most one `> Tip:` line per run" invariant is unchanged.

## Capabilities

### New Capabilities
- `post-start-tips`: Which single tip (if any) is shown after an emulator start: priority tiers, detector-backed tips and their one-detector-per-run budget, and the deploy tip.

### Modified Capabilities

## Impact

- `internal/container/tips.go` — tips become values with an eligibility check and an optional detector. Selection walks tiers and enforces the one-detector budget, deadline and panic recovery. Detector tip constructors (starting with `NewDeployTip`) live here too, so `container` now imports `internal/extension`.
- `internal/container/start.go` — `StartOptions` gains `DetectorTips []Tip`. `Start` passes the post-start facts (emulator type, first run, interactivity) into tip selection.
- `internal/extension/` — an `InvokeQuietlyWithJSON` helper, the counterpart to `Invoke`, that runs an extension on lstk's own behalf: captured output, always JSON and non-interactive, telemetry disabled, returning its envelope `data`.
- `cmd/root.go` — `buildStartOptions` assembles `DetectorTips` (resolver, config dir, and `cfg.AuthToken`, the same token extension dispatch conveys), so every `Start` caller gets them.
- Tests: unit tests for deterministic selection; an integration test with a compiled fake `lstk-deploy` on `PATH` (via `installFakeTool`) that records invocations.
- Depends on the `lstk deploy detect --json` envelope contract (`data.tools`: always-present string array) shipped by `lstk-bundled-extensions`.
