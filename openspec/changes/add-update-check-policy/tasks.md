## 1. Classify externally managed installs

- [x] 1.1 Extend `TestClassifyPath` with a `wantManager` column, keeping the mise-npm and asdf-npm rows classified as npm (the `273738e` regression), and add rows for nix store, `.nix-profile`, a directory merely named `nix`, mise, asdf, Scoop, Chocolatey, and a Windows npm path (`internal/update/install_method_test.go`)
- [x] 1.2 Pin the user-facing `DisplayName()`/`UpgradeHint()` strings per manager in `TestExternalManagerHints`, including the deliberately command-less Nix and asdf wording
- [x] 1.3 Add `InstallExternal`, `ExternalManager` with its constants and two methods, and `InstallInfo.Manager`/`ExternallyManaged()`; change `classifyPath` to return `InstallInfo` and match lstk's own methods before any manager segment (`internal/update/install_method.go`)
- [x] 1.4 Replace the `os.PathSeparator` split with `splitPathSegments`, splitting on both separators so Windows paths classify (and test) on Linux

## 2. Add the policy enum

- [x] 2.1 `TestParseCheckMode`: the three values, the trimmed/upper-case forms, and the exact error message for an invalid one (`internal/update/check_mode_test.go`)
- [x] 2.2 Add `CheckMode`, its constants, `CheckModes` and `ParseCheckMode` (`internal/update/check_mode.go`), documenting why it trims/lowercases where `ParseEmulatorType` does not
- [x] 2.3 Add the raw `CLIConfig.UpdateCheck` field with a doc comment on why it is not validated in `Get()`, plus `config.SetUpdateCheck` (`internal/config/config.go`)
- [x] 2.4 Add `Env.UpdateCheck` read from `update_check` (`internal/env/env.go`)

## 3. Apply the policy on the start path

- [x] 3.1 Migrate the notify unit tests from `UpdatePrompt` to `Mode`, and rewrite the dev-build test to assert no request is made rather than only that nothing is returned (`internal/update/notify_test.go`)
- [x] 3.2 New notify unit tests: `off` makes no request, a zero-value `Mode` notifies rather than prompting, the note names the manager, the 4th option is hidden without `PersistCheckMode`, choosing it persists `notify` and confirms, and a persist failure warns
- [x] 3.3 Replace `NotifyOptions.UpdatePrompt` with `Mode` plus `Manager`, `ConfigPath` and `PersistCheckMode`; return before the version check when `Mode` is `off`; add `notifyLine` and the `"n"` branch (`internal/update/notify.go`)
- [x] 3.4 Delete the exported `CheckQuietly` — it had no production callers
- [x] 3.5 `TestResolveUpdateCheckMode`: precedence in both directions, the externally-managed default, explicit overrides of it, the non-interactive downgrade, and the exact warning text for invalid values (`cmd/update_check_test.go`)
- [x] 3.6 Add `resolveUpdateCheckMode` and `buildNotifyOptions` (`cmd/update_check.go`)
- [x] 3.7 Build the options once in `startEmulator` and use them on both paths, fixing the non-interactive path's dropped `SkippedVersion` (`cmd/root.go`)

## 4. Refuse to self-update a manager-owned binary

- [x] 4.1 Add `ErrUpdateExternallyManaged` to the const block, `allErrorCodes` and `categoryByCode`, and bump the count assertion (`internal/output/error_code.go`, `error_code_test.go`)
- [x] 4.2 Integration test `TestUpdateRefusesExternallyManagedInstall`: exit 1 with the manager named, nothing on stderr, zero requests to the mock GitHub, the binary unchanged, the `UPDATE_EXTERNALLY_MANAGED` envelope, and `--check` still working — including with `LSTK_UPDATE_CHECK=off` (`test/integration/update_test.go`)
- [x] 4.3 Add `refuseExternalUpdate`, called from `Update` before `Check` when not `--check`, plus the `InstallExternal` case in `applyUpdate` as defense in depth (`internal/update/update.go`)

## 5. End-to-end coverage of the policy

- [x] 5.1 Add `mockGitHubReleaseServerCounting` and `lstkAtInstallPath` helpers, and the `LSTK_UPDATE_CHECK` key (`test/integration/update_test.go`, `test/integration/env/env.go`)
- [x] 5.2 Integration tests for each mode, the env override in both directions, an invalid value, the externally-managed default, "Don't ask again" (persistence, comment preservation, and effect on the next run), and the skipped-version fix on the non-interactive path (`test/integration/update_check_test.go`)

## 6. Documentation

- [x] 6.1 Add a commented `[cli]` section documenting `update_check` to `internal/config/default_config.toml` — it must stay commented, or it would outrank the externally-managed default for every config created after this change
- [x] 6.2 Extend `lstk update`'s `Long` with the refusal and the "always checks" clarification (`cmd/update.go`)
- [x] 6.3 Add the error code to `docs/structured-output.md`: table row, `USAGE` bucket, and `lstk update`'s Codes line
- [x] 6.4 Update the README self-update bullet and the CLAUDE.md Configuration (env var) and `internal/update/` sections
