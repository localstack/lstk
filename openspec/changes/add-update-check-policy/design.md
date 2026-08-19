## Decisions

### Three values, not a boolean

A boolean opt-out was the obvious shape, and it is the wrong one. The reporter asked for a one-line hint, not silence: *"I would prefer a simple notification — a one line hint that a new version is available, and it is on you to update, without any blocks / prompts. I think no notification at all also isn't the best thing, since people tend to forget to update, even if managed with a package manager."* A boolean forces a choice between the blocking prompt and no signal at all, so neither value is what was asked for. `notify` is the value the ticket is actually about; `off` exists for air-gapped and CI use, where suppressing the network request matters more than the notice.

### The enum lives in `internal/update`

`update.CheckMode` and `ParseCheckMode` sit with the domain that owns the format, matching `snapshot.MergeStrategy`. `internal/validate` was considered and rejected: that package classifies hostile input (control characters, traversal, shell metacharacters) with rule codes, and neither existing enum (`config.EmulatorType`, the merge strategies) lives there.

`config.CLIConfig.UpdateCheck` stays a raw `string`. `config.Get()` is called by every command, so validating there would let one unusable setting break the whole CLI — and `config` would have to import `update`.

### `ParseCheckMode` is lenient about case and whitespace

`config.ParseEmulatorType` matches exactly. `ParseCheckMode` trims and lowercases first, a deliberate divergence: this value arrives from a hand-typed shell line or CI env file where `Off` is a plausible spelling, and the set of three values admits no ambiguity. The cost is one inconsistency between two enum parsers; the benefit is that `LSTK_UPDATE_CHECK=OFF` does what it says.

### An invalid value warns and falls through; it never fails a command

The setting governs a best-effort background check. Hard-failing `lstk start` because `update_check` is misspelled would make an opt-out mechanism into a way to break the CLI, and there is no `lstk config set` to fix it with — the user must hand-edit the file, which lstk cannot help with while refusing to run. Resolution therefore reports each unparsable source through the sink and continues to the next one, ending at the install-implied default. `SeverityWarning` messages are already collected into the JSON envelope's `warnings[]`, so the diagnostic is machine-readable without new plumbing.

Falling through source by source, rather than jumping straight to the default, keeps the documented precedence literally true even when one source is garbage.

### `prompt` is downgraded to `notify` off a terminal

Only the TUI answers a `UserInputRequestEvent`; `PlainSink` renders the prompt and never feeds `ResponseCh`. A `prompt` policy on a non-interactive run would block until context cancellation with nothing actionable on screen. The downgrade is applied during resolution so it is a property of the resolved policy, not a special case buried in the domain. `notifyUpdateWithVersion`'s fallthrough branch is the notifying one for the same reason: a zero-value `Mode` must never block.

### Detection is path-only

`classifyPath` matches path segments and nothing else. A write-permission probe was considered — it would catch Nix, distro packages, and read-only container images generically — and rejected: it cannot distinguish a root-owned `/usr/local/bin` (self-managed, needs `sudo`) from a manager-owned directory, and those two need different advice. Conflating them would produce "update it with your package manager" for a user who has no package manager. Nix, the only read-only case in the ticket, is caught by its store path anyway.

The ordering inside `classifyPath` is load-bearing. `caskroom` and `node_modules` are matched **before** any manager segment, because `~/.local/share/mise/installs/node/24.8.0/lib/node_modules/@localstack/lstk_*/lstk` is a self-installed npm lstk under a mise-managed *Node.js* — an npm install that should keep updating through npm, which is exactly what commit `273738e` fixed. Reversing the two loops would regress it.

Most manager segments require a specific following segment (`mise/installs`, `scoop/apps`, `nix/store`, …), so a directory merely named `mise`, `scoop` or `nix` — far more likely a checkout of that tool than an lstk install by it — is not misread. This asymmetry is deliberate: a false negative leaves the user with today's behaviour, while a false positive makes `lstk update` refuse and advise a command that does not exist on their machine. Only the dot-prefixed names (`.asdf`, `.nix-profile`) are conclusive alone.

NixOS system profiles and per-user profiles resolve through the existing `filepath.EvalSymlinks` into `/nix/store`, so matching the store covers them without enumerating profile paths. A `mise` install under a custom `MISE_DATA_DIR` with no `mise` segment in the path is a known false negative; the explicit `update_check` setting covers it.

Segments are split on both `/` and `\` regardless of host OS. Beyond correctness on Windows, this is what makes Windows paths (Scoop, Chocolatey) testable on the Linux-only unit-test job.

### "Don't ask again" persists `notify`, not `off`

The user pressing it asked not to be interrupted, not to stop hearing about releases. The confirmation names the file written and says how to reach the other two values, so `off` remains one edit away.

The option is **hidden** when there is no config file yet. `config.Set` silently no-ops without one, and `config.EnsureCreated()` has exactly three legitimate callers — adding a fourth here would persist a default config on first run and permanently suppress the emulator picker. Withholding the option beats offering one that does nothing.

### Per-manager upgrade commands, except where they would be a guess

`mise upgrade lstk`, `scoop update lstk` and `choco upgrade lstk` are unambiguous. Nix and asdf are not: a Nix install may be a profile, a `nixos-rebuild` generation or home-manager, and asdf has no `upgrade` verb (nor an lstk plugin). Those two get "update it with Nix" / "update it with asdf" — naming the manager without printing a command that would fail.

### `UPDATE_EXTERNALLY_MANAGED` is a new code

`error_code.go` says a call site with no applicable code SHALL use `ErrInternal` rather than inventing one. This is a call site with no applicable code and a stable, actionable meaning worth branching on, so the code is added instead: a deliberate refusal is not an internal failure, and `USAGE_ERROR` would make it indistinguishable from a bad flag. Category `USAGE` — the invocation itself has to change.

### `applyUpdate` refuses too

`Update` refuses before `Check`, so the normal path never reaches the updater. `applyUpdate` still rejects `InstallExternal` because the prompt's "Update now" also routes through it, and a user who forces `update_check = "prompt"` on a managed install must still never have their binary replaced.

## Non-Goals

- **No write-permission probe.** See above: different question, different advice.
- **No `--force` on `lstk update`.** Overwriting a manager-owned binary is not something to make easy; the manager's own command is the supported path.
- **No `lstk config set`.** Useful, but a new public command surface and a separate decision. The prompt's "Don't ask again" covers the discoverability need this change creates.
- **`update_check` does not gate explicit `lstk update`.** Running the command is a direct user action; the setting is about the automatic check on the start path only.
- **Homebrew and npm stay lstk-managed.** lstk drives those through `brew upgrade` / `npm install -g` rather than replacing files, so they are not "externally managed" in the sense that matters here.
- **No check throttling or caching.** A daily one-line note in `notify` mode is the requested behaviour, not a problem to rate-limit. Reducing the number of GitHub requests per start is a separate concern.
- **No new output event type.** The notice and the warning are `MessageEvent`s and the refusal is an `ErrorEvent`, so the event-parity checklist does not apply.
