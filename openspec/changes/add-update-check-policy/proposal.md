## Why

lstk offers no way to turn off its automatic update check. On an interactive `lstk start` it blocks on a prompt (Update now / Remind me next time / Skip this version), and with the weekly release cadence "skip this version" is not enough — a user on a `mise`-managed install reported being interrupted almost daily, and a `nix` user pointed out that lstk lives in a read-only store there and could never have upgraded itself anyway.

Underneath the missing switch is a second problem: lstk classifies every install it does not recognise as a plain binary it may overwrite. A `mise`, `asdf`, `Nix`, `Scoop` or `Chocolatey` install therefore either gets replaced in place — leaving that manager's registry pointing at a version it no longer installed — or fails mid-download with an opaque write error.

The original prompt had a "Never ask again" option backed by a top-level `update_prompt` boolean (#136); it was removed in #159 and nothing replaced it.

## What Changes

- **Add `[cli] update_check`** with three values: `prompt` (today's behaviour, the default), `notify` (a one-line note, no input wait) and `off` (no check at all — no network request, no output).
- **Add `LSTK_UPDATE_CHECK`**, which overrides the config file. Precedence is env var → config → the default implied by the install.
- **Classify externally managed installs.** Install detection gains an `external` method identifying the owning manager from the resolved binary path. lstk's own methods keep priority, so an npm-installed lstk under a mise-managed Node.js stays an npm install and keeps self-updating through npm.
- **Default externally managed installs to `notify`**, with the note naming that manager's own upgrade command instead of `lstk update`. An explicit `update_check` overrides this in both directions.
- **Refuse `lstk update` on an externally managed install** with an actionable error (new `UPDATE_EXTERNALLY_MANAGED` error code) before any network request. `lstk update --check` stays allowed — it is read-only.
- **Add a "Don't ask again" option to the prompt**, persisting `notify`, so the interrupted user can fix it in-flow without reading documentation.
- **Resolve the policy once** for both start paths. The non-interactive path previously built its own `NotifyOptions` and so ignored `cli.update_skipped_version` entirely; that is fixed as a consequence.

## Capabilities

### Added Capabilities

- `update-check-policy`: the three-valued automatic update-check policy, its resolution precedence, the externally-managed install class and its default, and the refusal of self-update for manager-owned binaries.

### Modified Capabilities

- `error-codes`: gains `UPDATE_EXTERNALLY_MANAGED` (category `USAGE`, non-retryable). No existing code changes.

## Impact

- **Touched code**: `internal/update` (`check_mode.go` new, `install_method.go`, `notify.go`, `update.go`), `cmd/update_check.go` (new), `cmd/root.go` (`startEmulator`), `cmd/update.go` (help text), `internal/config/config.go`, `internal/env/env.go`, `internal/output/error_code.go`.
- **Tests**: `internal/update/{check_mode,install_method,notify}_test.go`, `cmd/update_check_test.go`, `internal/output/error_code_test.go`, `test/integration/update_check_test.go` (new) and `test/integration/update_test.go`.
- **Docs**: `internal/config/default_config.toml` (a commented `[cli]` section — the file had none), `docs/structured-output.md`, `README.md`, and the CLAUDE.md Configuration and `internal/update/` sections.
- **Behaviour change for existing users**: none by default on a self-managed install. Users on mise/asdf/Nix/Scoop/Chocolatey stop being prompted and stop being able to self-update, which is the point.
