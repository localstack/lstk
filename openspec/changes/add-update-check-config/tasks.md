## 1. Config and environment plumbing

- [x] 1.1 Add `UpdateCheck string` to `config.CLIConfig` (`mapstructure:"update_check"`), plus an `UpdateCheckMode` string type with the `prompt`/`notify`/`off` constants and a parser.
- [x] 1.2 Reject an unrecognized `update_check` value in `config.Get()`, alongside the existing container and named-env validation, with an error naming the key and the accepted values.
- [x] 1.3 Add `config.SetUpdateCheck(mode string) error` in place of `SetUpdateSkippedVersion` (delegating to `Set("cli.update_check", …)`, same surgical line rewrite).
- [x] 1.4 Add `UpdateCheck string` to `env.Env`, read in `env.Init()` via `viper.GetString("update_check")` — it must be captured there, since `config.loadConfig` calls `viper.Reset()`.
- [x] 1.5 Document the setting as a commented `[cli] update_check` block in `internal/config/default_config.toml`.
- [x] 1.6 Unit tests: value parsing (each valid value, an invalid value, empty), and `SetUpdateCheck` preserving surrounding comments and formatting on a file with and without an existing `[cli]` section.

## 2. External-install detection

Write the tests in this section before the implementation — 2.1's reordering is a regression risk to an existing passing test case.

- [x] 2.1 Unit tests for `classifyPath`: each manager in the design.md table resolves to `InstallExternal` with the right `Manager`; the existing `…/mise/installs/node/…/node_modules/@localstack/lstk_darwin_arm64/lstk` case still resolves to `InstallNPM`; `/usr/local/bin/lstk` and `/home/user/bin/lstk` still resolve to `InstallBinary`.
- [x] 2.2 Add `InstallExternal` to `InstallMethod` (with its `String()` case) and a `Manager string` field to `InstallInfo`.
- [x] 2.3 Reorder `classifyPath`: scan the whole path for the npm/Homebrew markers first, then for the tool-manager markers, so npm and Homebrew always win. Cover nix (`/nix/store/` prefix), guix (`/gnu/store/`), mise (`mise/installs`, `mise/shims`, legacy `rtx`), asdf (`.asdf/installs`, `.asdf/shims`), scoop (`scoop/apps`), chocolatey (`chocolatey/lib`).
- [x] 2.4 Add a writability probe for the resolved executable's directory, used only from the `lstk update` path — never from the mode-default path. Implemented as a create-and-remove temp file rather than `unix.Access`: see design.md's "Correction made during implementation" (more accurate, cross-platform without build tags, and cost is irrelevant on that path). An indeterminate probe does not block.

## 3. Notification modes

- [x] 3.1 Replace `NotifyOptions.UpdatePrompt bool` with the resolved mode. Return before `checkQuietlyWithVersion` for `off`; keep the existing note for `notify`; keep `promptAndUpdate` for `prompt`.
- [x] 3.2 Consult detection only after the version check reports an update available and only when no explicit mode was supplied, downgrading `prompt` to `notify` for an externally-managed install. Pass detection in as a function on `NotifyOptions` so the unit tests do not depend on the test binary's own path.
- [x] 3.3 Add the `n` — "Never remind me" option to the `ActionChoice` in `promptAndUpdate`, persisting via a `PersistUpdateCheck` callback on `NotifyOptions`; emit a warning (not an error) when persisting fails, matching the existing skip-version behavior.
- [x] 3.4 Resolve the mode at the command boundary in `cmd/root.go` from `cfg.UpdateCheck` and `appConfig.CLI.UpdateCheck`, and wire `config.SetUpdateCheck` as `PersistUpdateCheck`. Both the interactive (`ui.Run`) and non-interactive `NotifyUpdate` call sites must pass the resolved mode — today the non-interactive one passes only `GitHubToken`.
- [x] 3.5 Unit tests: each mode's outcome (no fetch for `off`; note only for `notify`; prompt for `prompt`); detection downgrading `prompt` to `notify`; an explicit mode skipping detection; the `n` option persisting `notify` and applying no update; a persist failure warning but not failing.

## 4. Update command

- [x] 4.1 Integration test (before implementation): copy the built `bin/lstk` into a mise-shaped temporary path, run `lstk update` with an isolated `HOME`, and assert the non-zero exit and the message naming mise and the path. This works despite the test binary's `dev` version only because the refusal precedes the version check.
- [x] 4.2 Add `UPDATE_EXTERNALLY_MANAGED` to `output.ErrorCode`, with its `retryable: false` classification and category, and a row in `docs/structured-output.md`'s error table.
- [x] 4.3 Add the refusal to `update.Update` ahead of the `dev`-version early return and the version check: emit an `ErrorEvent` with the new code, the manager, the resolved path, and an action pointing at the manager; return a silent error.
- [x] 4.4 Add `--force` to `cmd/update.go` and thread it through to bypass the refusal; document on the flag that it exists because detection is a heuristic.
- [x] 4.5 Integration tests: `lstk update --force` from the same mise-shaped path proceeds past the refusal; `lstk update --json` from a nix-shaped path emits the envelope with `UPDATE_EXTERNALLY_MANAGED`.

## 5. Documentation

- [x] 5.1 Update `cmd/update.go`'s `Long` to state that `update_check` does not gate the explicit command, and to describe `--force`.
- [x] 5.2 Add the `update_check` resolution order and the detection behavior to CLAUDE.md only if the guidance spans packages; otherwise put it in doc comments on `UpdateCheckMode`, `classifyPath`, and the `--force` flag, per CLAUDE.md's "Maintaining This File" rules.
- [x] 5.3 Run `make test`, `make test-integration`, and `make lint`.

## 6. Discovered during implementation

- [x] 6.1 The `notify` note ends in "(run lstk update)", which refuses on an externally-managed install. When detection forced the downgrade, name the manager instead ("installed via mise — update it there"); notes reached any other way keep pointing at `lstk update`.
- [x] 6.2 `applyUpdate` returned `InstallMethod.String()`, so `lstk update --force` on an external install would emit `"method": "external"` — a value outside the documented homebrew/npm/binary enum. Added `appliedMethodName`, mapping InstallExternal to `binary` (the field says how the update happened, and --force performs a binary replacement).
- [x] 6.3 `lstk update --check` exempted from the refusal: it writes nothing and its answer is useful however lstk was installed.
- [x] 6.4 `internal/output`'s `TestErrorCode_AllErrorCodesIsComplete` hardcodes the code count; bumped 34 → 35 alongside the new code. `docs/structured-output.md` still says "the 29 codes above" in its category section, which was already stale before this change and is left alone.

## 7. Adversarial-review fixes

- [x] 7.1 BLOCKER: move the `blockSelfUpdate` guard into `applyUpdate` so the start-path prompt's "Update now" cannot replace an externally-managed binary, and stop prompting on such installs entirely (even under an explicit `prompt`).
- [x] 7.2 BLOCKER: offer "Never ask again" only when a config file exists, and make `config.SetUpdateCheck` fail rather than succeed in memory only — it previously told the user the preference was saved on a first run and wrote nothing.
- [x] 7.3 Run detection for every mode except `off` once an update is known to exist, so the note names the manager on the non-interactive path and under an explicit `notify` too.
- [x] 7.4 Classify the new config failures: `failGetConfig` for `config.Get` in `startEmulator`, and an `ErrorEvent{Code: ErrConfigInvalid}` for a bad `LSTK_UPDATE_CHECK` (both previously surfaced as `INTERNAL_ERROR` under `--json`).
- [x] 7.5 Add the missing markers: bare `asdf` (ASDF_DATA_DIR layout), `scoop/shims`, `chocolatey/bin` — the launcher directories actually on PATH.
- [x] 7.6 Guard `blockSelfUpdate` against an empty `ResolvedPath` (`filepath.Dir("")` is `"."`, so it probed the working directory), and add the `blockSelfUpdate` unit tests that were missing entirely.
- [x] 7.7 ~~Clear `cli.update_skipped_version` when persisting the opt-out~~ — superseded by section 9, which removes the skipped-version mechanism entirely.
- [x] 7.8 Add the end-to-end coverage that was missing for the headline behavior — `off` making no request, `notify` emitting a note, the env var overriding config, the note naming the manager, and both invalid-value paths reporting `CONFIG_INVALID` — using the existing `LSTK_UPDATE_GITHUB_API_ENDPOINT` mock-server hook and the ldflags version stamp. My earlier claim that this was impractical because the test binary reports `dev` was wrong; `test/integration/update_test.go` already does exactly this.
- [x] 7.9 Test-quality fixes: drop the vacuous `NotContains("UPDATE_EXTERNALLY_MANAGED")` assertion (codes never render outside JSON); stop using the exempt `--check` in the test that claims to exercise the guard; document the no-ldflags dependency in the `--force` test; skip the two `0500`-directory tests as root; clear `LSTK_UPDATE_CHECK` in the env test instead of reading the developer's environment.
- [x] 7.10 Delete `InstallMethod.String()`, dead since `appliedMethodName` took its only caller, along with the `TestInstallMethodStringIncludesExternal` case added earlier in this same changeset (which asserted the switch's own literal).
- [x] 7.11 Docs: add `guix` to the help text and `docs/structured-output.md`; describe both refusal reasons on `--force` and the `--check` exemption; fix the stale "29 codes" count (now 35) in the block this change already edits; add the new code to `json-output-schema`'s `error-codes` spec table; drop the commented `update_check` example from the config template, which `setInFile` would otherwise contradict by inserting the live key above it; trim the two CLAUDE.md paragraphs that restated doc comments added in this same changeset; add `LSTK_UPDATE_CHECK` to CLAUDE.md's environment-variable list.

### Deliberately not changed

- The `n` key for "Never ask again" is kept, with the label sharpened from "Never remind me". A reflexive `n` (meaning "no") does write config, but the write is non-destructive, reversible, and close to what someone dismissing a repeated prompt wants; `r` remains the true decline.
- ~~A skipped version still suppresses output in `notify` mode.~~ Superseded by section 9: the skipped-version mechanism is removed entirely, so neither the suppression nor the trap remains.

## 8. Second-round adversarial review fixes

- [x] 8.1 MAJOR: the round-1 blocker fix had no test — deleting `applyUpdate`'s `blockSelfUpdate` guard left the whole suite green, because `promptAndUpdate` called the un-injectable `DetectInstallMethod()`. Replaced `NotifyOptions.DetectExternal` with a single injectable `DetectInstall func() InstallInfo` (also removing the redundant second detection), and added `TestPromptUpdateNowIsRefusedWhenTheBinaryCannotBeReplaced`, mutation-verified to fail without the guard.
- [x] 8.2 `applyUpdate` now returns the blocker instead of emitting an `ErrorEvent` itself, so each caller chooses the severity: `lstk update` fails with the error event, the start-path prompt emits one warning. Previously pressing "Update now" on a read-only install dir left a permanent red failure block on screen (ErrorEvent sets `hideHeader` and persists) *plus* a duplicate "Update failed" warning, while the emulator started underneath it. Covered by `TestPromptRefusalEmitsNoErrorEvent`, also mutation-verified.
- [x] 8.3 Cover the two untested halves of 7.2: `config.SetUpdateCheck`'s no-file error (unit) and the `config.HasFile()` gate in `cmd/root.go` (a PTY integration test asserting the option is absent on a first run and present when config exists). Both mutation-verified.
- [x] 8.4 Nil-guard `case "n"`'s `PersistUpdateCheck` call rather than relying on an invariant established 40 lines earlier.
- [x] 8.5 A partial failure on "n" (mode saved, skipped version not cleared) no longer reports success — it warns and names the consequence and the key to clear.
- [x] 8.6 Scope the "detection does not run" spec requirement to the automatic start-path check; it contradicted the requirement that `lstk update` detect before the version check.
- [x] 8.7 Strengthen `TestNotifyUpdateNeverRemindPersistsNotifyAndAppliesNoUpdate` (the "applies no update" half rested only on `exit == false`); make the `off` tests answer prompts so a regression fails an assertion instead of deadlocking the suite.
- [x] 8.8 Nits: fix the import grouping in `notify_test.go`. The `[cli]` comment block was first moved above its header (lstk inserts written keys directly below the header, so the docs ended up beneath the value), then reduced on review to the file's own house style — a single commented `update_check` line with an inline comment, matching how `[[containers]]` documents its keys. The written value does land above that line; `TestSetUpdateCheckOnTheShippedTemplate` pins that the result is still valid TOML that reads back correctly, which is what actually matters.

### Deliberately not changed (second round)

- **`lstk update` probes writability twice** (the pre-check and `applyUpdate`'s guard). Keeping `applyUpdate` as the choke point is worth more than one saved ~50µs probe, and the pre-check is what keeps the refusal ahead of the download. Documented at the call site.
- **An invalid `LSTK_UPDATE_CHECK` is only rejected by `lstk start`.** Making it symmetric with the config key needs `env.Env` threaded through `initConfigDeferCreate`'s every call site, and full symmetry is unreachable anyway (`version` and `--help` never load config). The variable only affects the start path, and the start path validates it.
- **The first interactive run offers no opt-out** (no config file exists yet to write to). Correct per 7.2, and the run after it offers it.

## 9. Drop "Skip this version"

- [x] 9.1 Remove the `s` option and its handler from the prompt, leaving exactly three choices (`u`/`r`/`n`, with `n` still conditional on a writable config).
- [x] 9.2 Remove the mechanism behind it: `NotifyOptions.SkippedVersion`, `NotifyOptions.PersistSkipVersion`, the suppression check, `CLIConfig.UpdateSkippedVersion`, `config.SetUpdateSkippedVersion`, and the clearing logic added by 7.7. A leftover key in an existing config is inert (viper ignores unknown keys), so no migration is needed.
- [x] 9.3 Retarget the pre-existing `TestUpdateNotification` "skip" subtest at the surviving config-writing option and rename it — it tests that a prompt-driven write preserves the user's comments and formatting, not which preference is written.
- [x] 9.4 Update the specs, proposal, and design rationale; leave `internal/config/config_test.go`'s use of the key as generic `setInFile` fixture data alone.

## 10. Final review fixes

- [x] 10.1 MAJOR: cover `--force`'s bypass inside `applyUpdate` — mutation-proven unprotected, since the integration test for `--force` runs a `dev` build that short-circuits before `applyUpdate` is reached. Added a unit test pointing the download at a dead address and asserting no blocker is returned.
- [x] 10.2 MAJOR: cover the `--check` exemption (`update --check` from a mise-shaped path must not refuse); a regression there would have exited 1 on every externally-managed install with nothing failing.
- [x] 10.3 Correct CLAUDE.md, which still described detection as running only "when neither is set" and omitted the `--force` caveat — the opposite of the shipped rule after 7.1/7.3.
- [x] 10.4 Remove the dead `Unset` → `Prompt` normalization in `notifyUpdateWithVersion` (a semantic no-op: only `Notify` is ever tested), and fix the two doc comments claiming the domain layer distinguishes unset from prompt. It does not — detection decides either way.
- [x] 10.5 `isReadOnlyFSError`'s comment claimed Windows coverage. `syscall.EROFS` compiles there but is never produced, so write-protected Windows media falls through as indeterminate; ACL denials still refuse. Comment corrected rather than adding a Windows errno.
- [x] 10.6 `resolvedDir`'s comment implied macOS previously failed; it passed by substring accident. Reworded to say Windows requires the helper and macOS merely benefits from it.
- [x] 10.7 `assert.Contains(combined, "mise")` was satisfied by the install path itself; tightened to `"managed by mise"`. Dropped `startEnv`'s unused `configFile` parameter.
- [x] 10.8 Inject `DetectInstall` in the 11 unit-test option literals that fell back to the real detector, matching the field's documented rationale — mutation-verified that no unit test now depends on where the test binary lives.
- [x] 10.9 Nits: correct the `externalMarkers` comment (store entries have no launcher directory), drop the redundant `os.ErrPermission` check (same sentinel as `fs.ErrPermission`), fix `notify_test.go` import grouping (8.8 claimed this but missed it), and rename the "Never remind" test names to match the "Never ask again" label.

### Not changed

- `internal/ui/app_test.go`'s `{Key: "s", Label: "Skip this version"}` fixture is pre-existing arbitrary sample data for a component test, unrelated to this prompt.
- `cmd/root.go`'s invalid-value action always advises unsetting the env var, which would misdescribe a bad *config* key — unreachable, because `config.Get()` rejects that one statement earlier. The two validations are redundant on that path by design: `Get()` covers every command, the env branch covers only the start path.
- For an unwritable install directory the offered `--force` will itself fail at the rename. Spec-mandated: `--force` exists for the path-marker heuristic, and suppressing it per-reason would make the flag's contract conditional.

## 11. Review: simplify the config option to a boolean

- [x] 11.1 Replace `[cli] update_check` (`prompt`/`notify`/`off`) with `[cli] check_for_update_on_startup` (boolean, default true), and `LSTK_UPDATE_CHECK` with `LSTK_CHECK_FOR_UPDATE_ON_STARTUP`, per review. Detection alone now decides prompt vs note.
- [x] 11.2 `CLIConfig.CheckForUpdateOnStartup` is a `*bool` so an unset key stays distinguishable from an explicit false; the env var stays a raw string for the same reason.
- [x] 11.3 Validate a non-boolean config value before unmarshal: mapstructure's own failure names the key but neither the offending value nor the accepted ones, and the environment variable's message should match.
- [x] 11.4 `NotifyOptions.Mode` becomes `CheckEnabled bool`, deleting the `mode == notify` branch. `internal/update` no longer imports `internal/config`.
- [x] 11.5 The prompt's opt-out becomes "Never check again" and persists `false` — a stronger commitment than the previous "Never ask again" → notify; recorded in design.md.
- [x] 11.6 Retire the two unit tests for a state that no longer exists (explicit `notify` on a self-managed install): one becomes "an enabled check prompts a self-managed install", the other moves to the non-interactive path where the `lstk update` note still appears.

## 12. Interaction with #482 (bundled extensions)

- [x] 12.1 Restore `InstallMethod.String()`, deleted here as dead code while #482 landed a new caller in parallel. Documented alongside `appliedMethodName`, which names how an update was *performed* for the `--json` envelope rather than how lstk was installed.
- [x] 12.2 `detectMissingBundle` guarded on `InstallBinary`, so an externally-managed install silently lost the missing-bundle hint #482 introduced. It now covers `InstallExternal` and names the managing tool instead of pointing at a release download, which would install outside that tool.
