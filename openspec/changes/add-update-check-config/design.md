# Design

## Is external management reliably detectable?

Partially — reliably enough to be a heuristic, not reliably enough to be the only mechanism. This was the open question behind the ticket's own framing ("if not, the easiest way might be to offer an option to never prompt again"), so it is settled first.

`DetectInstallMethod` already resolves symlinks (`filepath.EvalSymlinks`), which is what makes path markers viable — `~/.nix-profile/bin/lstk` and `/opt/homebrew/bin/lstk` both resolve into their real stores.

| Manager | Resolved path marker | Confidence |
|---|---|---|
| nix | `/nix/store/…` prefix | unambiguous |
| guix | `/gnu/store/…` prefix | unambiguous |
| mise | `mise/installs/…`, `mise/shims/…` (also legacy `rtx/…`) | high |
| asdf | `.asdf/installs/…`, `.asdf/shims/…` | high |
| scoop | `scoop/apps/…` | high |
| chocolatey | `chocolatey/lib/…` | high |

A real mise install looks like `/Users/<user>/.local/share/mise/installs/github-localstack-lstk/latest/lstk`.

**What it cannot catch, ever.** Distro packages (`/usr/bin/lstk` from apt/dnf/AUR), a `COPY` in someone's Dockerfile, or any manager not in the table. False negatives are permanent, which is the whole reason detection cannot be the only mechanism: the authoritative control has to be an explicit, documented setting. Hence both, not either.

**The ordering trap.** `classifyPath` walks path segments in order and returns on the first marker it recognizes. The existing test case `/Users/someone/.local/share/mise/installs/node/24.8.0/lib/node_modules/@localstack/lstk_darwin_arm64/lstk` is npm-installed lstk under a mise-managed *node* — `mise` appears before `node_modules`, so adding mise to the same in-order walk silently reclassifies a working npm install as unmanageable. `npm install -g` is perfectly fine there. So the npm/Homebrew markers are checked across the whole path first, and the tool-manager markers only if neither matched.

**Writability as a backstop, not a primary signal.** Probing whether the resolved executable's directory is writable catches nix, read-only containers, and a root-owned `/usr/bin` run as non-root. It does *not* catch mise, whose install directory is writable — so it complements the path markers rather than replacing them. It is used only on the explicit `lstk update` path (see below), never to pick a default.

## Overhead, and why detection is lazy

Detection is cheap but not free, and the naive placement — resolving the mode at the `cmd/` boundary — would pay it on every `lstk` / `lstk start`. Measured on an M5, warm page cache, each operation in isolation:

| Step | Cost |
|---|---|
| `os.Executable()` | 5.7 ns on macOS; a `readlink("/proc/self/exe")` on Linux, ~1–2 µs |
| `EvalSymlinks` on an 8-component mise path | 18 µs |
| `EvalSymlinks` on `/usr/local/bin` | 3.6 µs |
| `classifyPath` segment walk | 377 ns |
| `unix.Access(dir, W_OK)` | 5 µs |
| temp-file writability probe | 49 µs, and it writes into the executable's directory |
| full detection on a shallow path | 7.4 µs |

`EvalSymlinks` dominates because it `lstat`s every path component, so cost grows with install depth.

Worst realistic case is ~25 µs, which is negligible against an HTTPS round-trip to the GitHub API under a 2 s timeout — but only if it is actually on that path. Two facts keep it there:

1. The update notification runs only in `startEmulator` (`cmd/root.go`, `internal/ui/run.go`). Bare `lstk` and `lstk start`, nothing else — `lstk aws …`, `status`, `logs`, `stop` never reach it.
2. Detection computes only a *default*, so it is skipped whenever `LSTK_UPDATE_CHECK` or `[cli] update_check` is set, returns before anything for `off`, and is otherwise consulted only once the version check has already reported an update available.

Net: zero added cost on every command except `lstk`/`lstk start`, and on those, cost only in the branch where an update actually exists and the user has expressed no preference.

The writability probe is deliberately excluded from that path. Path markers alone cover the managers in the table; the probe is only worth its cost on the explicit `lstk update` refusal, where nothing is time-sensitive. That also removes the objection that lstk would write a temp file into its own install directory on every start.

**Correction made during implementation.** This section originally argued for `unix.Access` (5 µs) over the temp-file probe (49 µs) on the update path. That reasoning does not survive its own premise: the same paragraph establishes that cost is irrelevant there, and the temp file is both more accurate (`access()` can report success for root, or under an ACL that the subsequent rename still fails) and cross-platform without a build-tagged Windows variant. The implementation uses the temp file, removes it immediately, and treats an indeterminate probe as "do not block", so an unrelated I/O error can never refuse an update that would have worked.

## Why three modes rather than a boolean

The two stakeholders in the ticket want different things. The reporter asked to "completely disable the update check" — no notification at all. The ticket's own suggested resolution is the opposite: keep checking and show an info label, but never block. A boolean cannot express both, and collapsing them would ship the wrong one for somebody.

- `prompt` — today's behavior. Default, because most installs are self-managed and the prompt is how updates actually reach users.
- `notify` — one line through the sink, no `ActionChoice`, never blocks. Also the automatic default for a detected externally-managed install.
- `off` — returns before the network request. Genuinely silent, not "silent but still phoning home", because a user disabling update checks on a locked-down or air-gapped machine means the request too.

Two overlapping booleans (`disable_update_prompt` + `disable_update_check`) were rejected: the four combinations include one that means nothing, and the names invite reading them as independent when the second subsumes the first.

## Why the setting does not gate `lstk update`

`lstk update` is a direct request; `update_check` describes an unsolicited interruption. Gating the explicit command on it would mean a user who set `off` has no way to update at all short of editing config back, and would make the setting's meaning depend on how it was reached. So the setting scopes to the automatic check only, and `lstk update` always runs.

Detection is the one exception, and for a different reason: there the refusal is not about noise but about the action being wrong. It still yields to `--force`, so detection never blocks a user who knows better than the heuristic.

## Why the refusal comes before the version check

`Update` checks for an externally-managed install first, ahead of the `version.Version() == "dev"` early return and the network call — but only when the update would actually be applied. `lstk update --check` is exempt: reporting whether a newer version exists is useful however lstk was installed and writes nothing, so refusing it would remove information for no gain.

Two reasons for the ordering:

- It avoids the current nix behavior of paying for a full download and checksum verification before discovering the target is read-only.
- It makes the refusal observable end to end. Integration tests run the freshly built `bin/lstk`, whose version is `dev`, and every existing update path returns early on that — so nothing downstream of the version check can be covered by an integration test today. With the refusal first, a test can copy `bin/lstk` into a mise-shaped temporary path, run `lstk update`, and assert the message and exit code through the CLI, which is what the repo's testing rules ask for.

## Why "Never remind me" persists `notify` rather than `off`

The prompt option is the fix for discoverability, and its job is to stop the interruption the user just experienced — not to decide that they never want to know about a release again. `notify` does exactly what was asked and leaves a one-line trail pointing at `lstk update`; `off` is a stronger, quieter choice that belongs in a config file the user wrote deliberately, not behind a single keystroke pressed to dismiss a prompt.

## Deliberate non-goals

- **No new `lstk config set` surface.** `SetUpdateCheck` follows the `SetUpdateSkippedVersion` pattern it replaces and is called from the prompt path only. A general config-writing command is a separate concern.
- **No attempt to run the external manager's own update.** `mise upgrade` exists but its semantics depend on the backend the user configured, and nix has no single equivalent. Naming the manager and the resolved path is honest and actionable; guessing a command is neither.
- **No rewriting of existing config files** to add the new commented block. `default_config.toml` is only ever written on first run, and CLAUDE.md is explicit that only a real emulator start may create it.

## Added during implementation

**The note names the manager.** The `notify`-mode line ends in "(run lstk update)", which is wrong advice on an externally-managed install — that command now refuses. When detection is what downgraded the mode, the note names the manager instead ("installed via mise — update it there"). A note reached any other way keeps pointing at `lstk update`.

**`method` in the JSON envelope stays a closed enum.** `applyUpdate` reported `InstallMethod.String()`, so a `--force` update on an external install would have emitted `"method": "external"` — a new value in a field documented as homebrew/npm/binary. The field describes how the update was performed, and `--force` performs a binary replacement, so `appliedMethodName` maps it to `binary`.

## Corrections after adversarial review

Two defects in the first implementation defeated the feature's purpose; both are now covered by tests that were mutation-checked.

**The prompt's "Update now" bypassed the guard.** `blockSelfUpdate` was wired only into `Update()`, while `promptAndUpdate` called `applyUpdate` directly — and `applyUpdate`'s switch falls through to the binary updater for `InstallExternal`. With an explicit `update_check = "prompt"`, detection was skipped, the prompt appeared on a mise install, and pressing `u` overwrote the mise-owned binary: verbatim the motivating bug. Fixed twice over: the guard moved into `applyUpdate` (the single choke point every replacing path goes through), and an externally-managed install is now never prompted at all, even under an explicit `prompt`.

**"Never ask again" persisted nothing on a first run.** `config.Set` succeeds in memory only when no config file has been resolved, and the interactive path notifies before the emulator picker creates the file — so the user was told the preference was saved, nothing was written, and the next start prompted again. Rather than adding a fourth `EnsureCreated` caller (CLAUDE.md enumerates exactly three, and eager creation would rob the emulator picker of its first run), the option is now offered only when a config file exists, and `SetUpdateCheck` fails loudly instead of silently succeeding.

**Detection is no longer skipped for explicitly-set modes.** The original "explicit mode skips detection" rule produced a note advising `lstk update` on installs where that command refuses — including on every non-interactive start, which short-circuited before detection entirely. Detection now runs once an update is known to exist, for every mode except `off`. The performance intent is unchanged: it stays off every start where no update exists, which is the overwhelming majority.

**Marker coverage was wrong for the paths actually on `PATH`.** `.asdf` missed the `ASDF_DATA_DIR` layout (`~/.local/share/asdf`), and scoop and chocolatey were matched only at their install roots, not at the `shims`/`bin` launcher directories that are what `PATH` actually points at — and whose entries are not symlinks, so `EvalSymlinks` does not rewrite them. Each miss silently clobbered a managed install.

## Why "Skip this version" is removed rather than kept

The prompt would otherwise offer four options, three of which mean "no": remind me later, skip this one, never ask. They are genuinely distinct, but the middle one earns the least:

- It is the option the ticket says does not work. Against a weekly cadence, "skip until the next version" buys days.
- It is the only per-version persisted state in the feature, and that state suppressed output in *every* mode — so a stale skip silenced the note `notify` mode promises, for exactly the release the user was most likely to see next. Closing that trap needed "Never ask again" to clear the skipped version, i.e. extra machinery whose only purpose was to undo the option being removed.
- "Remind me next time" already covers "not now", and the permanent opt-out covers "stop".

Removing it drops `cli.update_skipped_version`, `config.SetUpdateSkippedVersion`, `NotifyOptions.SkippedVersion`, `NotifyOptions.PersistSkipVersion`, and the clearing logic. A leftover `update_skipped_version` key in an existing config becomes inert rather than an error — viper ignores unknown keys, so no migration is needed. Someone mid-skip gets prompted once more for the version they skipped, and can then say "never" instead, which is what they wanted.

This also means nothing in automation is affected: `lstk start --non-interactive` never prompts — it emits a note and continues — so no scripted or toolkit flow depends on the prompt's option set.

`internal/config/config_test.go` still uses `cli.update_skipped_version` as the fixture key for `setInFile`, which is a generic "write one section.field" helper. Those tests are unchanged deliberately: the key is arbitrary test data there, and rewriting a dozen pre-existing assertions would be churn unrelated to this change.

## Open question for review: where the update prompt sits in the start flow

`internal/ui/run.go` calls `NotifyUpdate` as the first action of the start goroutine — ahead of the Docker health check, the auth flow, and the emulator picker. One consequence is that "Never ask again" cannot be offered on a genuine first run: `config.toml` does not exist yet (the picker creates it), so the preference would have nowhere to go, and the option is therefore hidden there.

Moving the notification after the picker would let the option appear on every run. **It is deliberately not moved**, because prompting early is worth more than that: a user on an old or broken CLI should be offered the update before the CLI attempts real work, which matters for both stability and usability. A late prompt would also be preempted by a Docker failure, i.e. exactly the situation where updating might be the fix.

The residual gap is narrow — a first run means no config, which almost always means a fresh install already on the latest version, so there is usually nothing to prompt about. Raised here as an open question for the PR rather than settled unilaterally.
