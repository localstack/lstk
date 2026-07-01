## 1. Extension package scaffolding

- [x] 1.1 Create `internal/extension/` package with an `Extension` struct (resolved name, executable path) and constructor; use `log.Nop()` in tests
- [x] 1.2 Define and document `LSTK_EXT_API_VERSION` and the `LSTK_EXT_CONTEXT` JSON contract — Go types for the context object (config dir, auth token, non-interactive, `emulators` array of `{type,endpoint,port}`) and a package doc comment (reworked from the flat `LSTK_EXT_*` variable set per Decision 3)
- [x] 1.3 Unit tests for the package's basic types/helpers

## 2. Discovery and resolution

- [x] 2.1 Implement `Resolve(name)` searching the bundled dir (next to the symlink-resolved lstk executable) first, then `PATH`, for `lstk-<name>`; honor Windows executable extensions; return first match (bundled wins)
- [x] 2.2 Implement `List()` scanning bundled dir + `PATH` for `lstk-*` executables (names), de-duplicating by command name with bundled-then-PATH precedence
- [x] 2.3 Implement bundled-dir resolution from `os.Executable()` with symlink resolution (works through npm/Homebrew shims)
- [x] 2.4 Unit tests for resolution order (bundled wins), PATH fallback, not-found behavior, Windows extension handling, List de-duplication, and symlink-resolved bundled-dir lookup

## 3. Runtime context contract (JSON object — reworked per PR review, Decision 3)

> Reworked from the original flat `LSTK_EXT_EMULATOR_*` variables to a single versioned JSON object so multiple simultaneously-running emulators are representable from day one. The previously-implemented flat-variable builder must be replaced.

- [x] 3.1 Implement a builder that produces two environment variables layered on the inherited host env: `LSTK_EXT_API_VERSION` (flat integer) and `LSTK_EXT_CONTEXT` (a JSON object with `configDir`, optional `authToken`, `nonInteractive`, and `emulators`); strip any inherited `LSTK_EXT_*` first
- [x] 3.2 Populate `emulators` as a JSON array of `{type, endpoint, port}` for **all** running emulators via `internal/endpoint` + `internal/container` discovery (not just one); use `[]` when none are running (wired at the command boundary in `cmd/extension.go`)
- [x] 3.3 Include `authToken` in the object only when a token is resolved (omit the field otherwise); always include `configDir`; no entitlement/grant fields
- [x] 3.4 Set the `nonInteractive` field from lstk's resolved interactivity (the `isInteractive` condition: `--non-interactive` given or stdout not a TTY); document that future global flags are conveyed as additive JSON fields
- [x] 3.5 Unit tests asserting the JSON shape across scenarios: zero/one/multiple emulators, authed vs not (field present/absent), non-interactive flag vs non-TTY, host env inherited, stray `LSTK_EXT_*` stripped
- [x] 3.6 Record each **resolved** extension invocation in product telemetry: `dispatchExtension` emits an `lstk_command` event named `ext:<name>` (duration + exit code) via `telemetry.Client`, so the warehouse tracks which extension ran; `instrumentCommands` skips its generic emit for the extension-dispatch path so it is not mislabeled `start`, and an unresolved command records nothing. `extension.Invoke` additionally opens an OTel tracing span (name, bundled, exit code); no trace-context injection into the child (Decision 8)

## 4. Invocation (exec) path

- [x] 4.1 Implement `Invoke(extension, args, ctx)` that builds the runtime env, execs the extension with args forwarded unmodified, passes stdin/stdout/stderr through, and propagates the exit code (model on `internal/iac/.../cli/exec.go`)
- [x] 4.2 Ensure non-zero extension exits propagate without an extra lstk-level error message
- [x] 4.3 Unit/integration tests for argument forwarding, stream passthrough, and exit-code propagation using a reference extension

## 5. Command wiring and dispatch

- [x] 5.1 Wire unknown-command dispatch in `cmd/root.go`: when Cobra finds no built-in/alias for `<name>`, attempt bundled+PATH resolution and invoke; built-ins always take precedence
- [x] 5.2 Apply `SetInterspersed(false)` so lstk's global flags are parsed only before the command name and everything from `<name>` onward is forwarded verbatim; verify it doesn't disturb bare-root `start` or built-in subcommand flags (fall back to a `stripGlobalFlags`-style pass if needed)
- [x] 5.3 Ensure extension args are not parsed by lstk (`SetInterspersed(false)` makes everything from `<name>` onward opaque, giving the same effect as `DisableFlagParsing` for the synthesized path)
- [x] 5.4 Add extensions to `lstk help` under an "Extensions" grouping by scanning bundled dir + `PATH` for `lstk-*` (de-duplicated, bundled wins)
- [x] 5.5 Read the bundled descriptions file (name → one-liner) shipped in the bundled dir and attach descriptions to bundled extensions in help; `PATH`/custom extensions and bundled names missing from the file are name-only; a missing/unreadable file degrades to name-only without error; never execute an extension during help
- [x] 5.6 Wire config initialization only where needed (extension dispatch needs config dir/endpoint); keep side-effect-free paths unaffected
- [x] 5.7 Integration tests: built-in precedence, unknown→extension, unknown with no extension errors, help listing showing bundled descriptions from the file, PATH extensions name-only (and not executed during help), missing-descriptions-file degrades to name-only, and `lstk --non-interactive <ext>` conveying `nonInteractive: true` in `LSTK_EXT_CONTEXT` while not forwarding the flag (update the non-interactive assertion for the JSON contract)

## 6. Reference extension and end-to-end coverage

- [x] 6.1 Add a small reference/example `lstk-*` extension used by tests that reads `LSTK_EXT_API_VERSION`, decodes `LSTK_EXT_CONTEXT` JSON, and echoes the decoded fields (config dir, auth token, non-interactive, emulators) so tests can assert on them
- [x] 6.2 Integration test: place the reference extension on a test `PATH`, invoke via `lstk <name>`, and assert it received the expected JSON context (an `emulators` entry when an emulator is running, `[]` when none, `authToken` when authed)

## 7. Bundled extension resolution (first release)

> Distribution + atomic update (the former 7.1–7.4) are **deferred to the `add-bundled-extension-distribution` change** per the phased launch: the first release resolves and runs a bundled extension placed manually, but does not package or auto-update bundled extensions. The previously-implemented `internal/update` atomic-set logic, `scripts/check-descriptions.sh`, the GoReleaser archive block, and `docs/extensions-bundling.md` were removed from this change and are re-introduced by the future change.

- [x] 7.5 Add a reference bundled extension (in-tree, for tests) that conveys its received context and performs a stubbed self-authorization, exercising the bundled path end to end. It lives under the test-sample tree, not advertised as a public example — `test/integration/test-samples/extensions/lstk-ref` (the prose author guide in `docs/extensions-authoring.md` replaces the `examples/lstk-ref` showcase)
- [x] 7.6 Integration tests: bundled extension resolvable when present, bundled wins over a same-named `PATH` extension, resolvable via a symlinked/shim `lstk`, bundled description shown in help (when a descriptions file is present), and bundled premium extension still self-authorizes

## 8. Documentation and finalize

- [x] 8.1 Add/update the "Extensions" section in CLAUDE.md: the mechanism, the `LSTK_EXT_API_VERSION` + `LSTK_EXT_CONTEXT` JSON contract (config dir, auth token, `nonInteractive`, `emulators` array), bundled-dir + PATH resolution, bundled-wins precedence, the hand-authored descriptions file (bundled-only help text), invocation telemetry, and the self-authorization model
- [x] 8.2 Write/update the public extension-author guide: the manifest-free contract, the `LSTK_EXT_CONTEXT` JSON schema (with the `emulators` array and how to handle multiple/zero emulators), global-flag conveyance via the context object, that help descriptions are bundled-only, how to authorize with the conveyed token, the security note that authorization must not rely on lstk, and the deferred items (signed entitlement, trace propagation, shared SDK) — `docs/extensions-authoring.md`
- [x] 8.3 Re-run `make lint`, `make test`, and `make test-integration` after the JSON-contract + telemetry rework; confirm the extension suite is green (pre-existing token/emulator/`az`-gated failures excepted)
