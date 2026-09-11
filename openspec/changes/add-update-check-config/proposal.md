## Why

`lstk` has no way to stop asking about updates. The only escape hatch today is "Skip this version" (`cli.update_skipped_version`), which the weekly release cadence invalidates within days — so a user on a fast-moving channel is prompted again almost every day (DEVX-1029, reported from Slack). That option is removed here rather than kept alongside the new one: it bought days, not quiet, and left the prompt with three ways to say "no".

The friction is worst where lstk is not responsible for its own installation. When lstk is installed by `mise`, `nix`, or `asdf`, updates are already governed by that tool's policy — a lockfile, a flake, a deliberately pinned version — and self-updating is not just unwanted but actively wrong. Two concrete failures exist today, both reachable from the current prompt:

- **nix:** the store path is read-only. `lstk update` downloads the release archive, verifies its SHA-256, and only then fails when `os.Rename` into `/nix/store` returns `EROFS` (`internal/update/extract.go:56-66`) and the `copyFile` fallback fails too. The user pays the full download for a guaranteed failure and a confusing error.
- **mise:** the install directory *is* writable, so the update **succeeds** — silently replacing the binary that mise has recorded a version for, desynchronizing lstk from the tool that manages it. This is exactly the "I don't want lstk to meddle with it" complaint in the report.

So two things are missing: an explicit, permanent opt-out the user can set once, and enough awareness of externally-managed installs that lstk stops offering an action that cannot work.

## What Changes

- Add a `[cli] check_for_update_on_startup` boolean (default true), with `LSTK_CHECK_FOR_UPDATE_ON_STARTUP` as the environment variable. A boolean rather than a three-mode enum because install detection already decides between a prompt and a non-blocking note; the only choice left to the user is whether to check at all.

  | setting | self-managed, interactive | externally managed or non-interactive |
  | --- | --- | --- |
  | `true` (default) | today's blocking prompt | one non-blocking note |
  | `false` | no check, no output, no request | no check |

- Resolution order: `LSTK_CHECK_FOR_UPDATE_ON_STARTUP` > `[cli] check_for_update_on_startup` > default `true`.
- The setting governs **only** the automatic check on the start path. An explicit `lstk update` / `lstk update --check` always checks and applies regardless of the setting — it is a direct request, not a background nag.
- Replace the prompt's "Skip this version" option with `n` — "Never check again", which persists `cli.check_for_update_on_startup = false`. This is the discoverability half of the fix; the reported problem was not that the prompt exists but that there is no way to say "stop" from where it appears. The prompt keeps three choices rather than gaining a fourth, and `cli.update_skipped_version` and its setter are removed.
- Detect externally-managed installs (`nix`, `guix`, `mise`, `asdf`, `scoop`, `chocolatey`) from the resolved executable path, and use that both to emit a note instead of a prompt and to make `lstk update` refuse rather than clobber — naming the manager and the resolved path instead of attempting an update. `lstk update --force` overrides the refusal, so a false positive is never a dead end.
- Detection is lazy and off the hot path: a disabled check returns before it, and it is otherwise consulted only after the version check has already reported an update available. See design.md for the measured costs.

## Capabilities

### New Capabilities
- `update-check-config`: the `[cli] check_for_update_on_startup` setting and `LSTK_CHECK_FOR_UPDATE_ON_STARTUP` variable, the enabled/disabled behavior and its exact output, the resolution order, the scope limit to the automatic check, and the "Never check again" prompt option that persists it.
- `external-install-detection`: the path markers that identify an externally-managed install, their precedence relative to the existing Homebrew/npm classification, the `lstk update` refusal and its `--force` override, and the requirement that detection never runs on invocations that do not reach the update check.

## Impact

- `internal/update/notify.go`: `NotifyOptions` gains a resolved mode instead of the `UpdatePrompt bool`; `notifyUpdateWithVersion` returns before the network call for `off`, and consults detection only after `checkQuietlyWithVersion` reports an update. `promptAndUpdate` gains the `n` option.
- `internal/update/install_method.go`: `InstallMethod` gains `InstallExternal`; `InstallInfo` gains a `Manager` string. `classifyPath` is reordered so `node_modules`/`Caskroom` win over the tool-manager markers — the existing test case `…/mise/installs/node/24.8.0/lib/node_modules/@localstack/lstk_darwin_arm64/lstk` (npm-installed lstk under a mise-managed *node*) must stay `InstallNPM`, and the current in-order segment walk would misclassify it.
- `internal/update/update.go`: `Update` checks for an externally-managed install before the version check (which also makes the refusal observable end-to-end without a non-`dev` build) and refuses unless `--force`. A writability probe of the resolved executable's directory backs up the path markers on this path only.
- `internal/config/config.go`: `CLIConfig` gains `CheckForUpdateOnStartup *bool` (a pointer so an unset key stays distinguishable from an explicit false) and loses `UpdateSkippedVersion`; a `SetCheckForUpdateOnStartup` setter replaces `SetUpdateSkippedVersion` (same surgical line rewrite); an invalid value is rejected in `Get()` alongside the container validation.
- `internal/env/env.go`: `Env` gains `CheckForUpdateOnStartup`, read in `Init()` — it must be captured there because `config.loadConfig` calls `viper.Reset()`.
- `cmd/root.go`: resolves the mode from env + `appConfig.CLI` at the command boundary and passes it into `NotifyOptions`; `cmd/update.go` gains `--force`.
- `internal/output/error_code.go`: a new `UPDATE_EXTERNALLY_MANAGED` code for the refusal, plus its `retryable`/`category` classification and a row in `docs/structured-output.md`.
- `internal/config/default_config.toml`: a commented `[cli] check_for_update_on_startup` line. Note this only reaches users whose config is created after this change — existing files are never rewritten — so `lstk docs` and the prompt option are the discovery paths for everyone else.
