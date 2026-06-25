## 1. Extension package scaffolding

- [ ] 1.1 Create `internal/extension/` package with an `Extension` struct (resolved name, executable path) and constructor; use `log.Nop()` in tests
- [ ] 1.2 Define and document the `LSTK_EXT_API_VERSION` integer constant and the full `LSTK_EXT_*` variable contract in a package doc comment
- [ ] 1.3 Unit tests for the package's basic types/helpers

## 2. Discovery and resolution

- [ ] 2.1 Implement `Resolve(name)` searching the bundled dir (next to the symlink-resolved lstk executable) first, then `PATH`, for `lstk-<name>`; honor Windows executable extensions; return first match (bundled wins)
- [ ] 2.2 Implement `List()` scanning bundled dir + `PATH` for `lstk-*` executables (names), de-duplicating by command name with bundled-then-PATH precedence
- [ ] 2.3 Implement bundled-dir resolution from `os.Executable()` with symlink resolution (works through npm/Homebrew shims)
- [ ] 2.4 Unit tests for resolution order (bundled wins), PATH fallback, not-found behavior, Windows extension handling, List de-duplication, and symlink-resolved bundled-dir lookup

## 3. Runtime context contract

- [ ] 3.1 Implement a builder that produces the `LSTK_EXT_*` environment (API version, emulator endpoint/type/port, config dir, auth token) layered on the inherited host environment
- [ ] 3.2 Wire emulator endpoint/type/port resolution via existing `internal/endpoint` + `internal/container` discovery; omit endpoint vars when no emulator is running
- [ ] 3.3 Include `LSTK_EXT_AUTH_TOKEN` only when a token is resolved; always include `LSTK_EXT_CONFIG_DIR`; do not set `LSTK_EXT_GRANT`/`LSTK_EXT_PUBLIC_KEY`
- [ ] 3.4 Set `LSTK_EXT_NON_INTERACTIVE` from lstk's resolved interactivity (the `isInteractive` condition: `--non-interactive` given or stdout not a TTY); document that future global flags are conveyed as additive `LSTK_EXT_*` vars
- [ ] 3.5 Unit tests asserting variable presence/absence across scenarios (emulator running vs not, authed vs not, non-interactive flag vs non-TTY, host env inherited)

## 4. Invocation (exec) path

- [ ] 4.1 Implement `Invoke(extension, args, ctx)` that builds the runtime env, execs the extension with args forwarded unmodified, passes stdin/stdout/stderr through, and propagates the exit code (model on `internal/iac/.../cli/exec.go`)
- [ ] 4.2 Ensure non-zero extension exits propagate without an extra lstk-level error message
- [ ] 4.3 Unit/integration tests for argument forwarding, stream passthrough, and exit-code propagation using a reference extension

## 5. Command wiring and dispatch

- [ ] 5.1 Wire unknown-command dispatch in `cmd/root.go`: when Cobra finds no built-in/alias for `<name>`, attempt bundled+PATH resolution and invoke; built-ins always take precedence
- [ ] 5.2 Apply `SetInterspersed(false)` so lstk's global flags are parsed only before the command name and everything from `<name>` onward is forwarded verbatim; verify it doesn't disturb bare-root `start` or built-in subcommand flags (fall back to a `stripGlobalFlags`-style pass if needed)
- [ ] 5.3 Ensure extension args are not parsed by lstk (`DisableFlagParsing` semantics for the synthesized extension path)
- [ ] 5.4 Add extensions to `lstk help` under an "Extensions" grouping by scanning bundled dir + `PATH` for `lstk-*` (de-duplicated, bundled wins)
- [ ] 5.5 Read the bundled descriptions file (name → one-liner) shipped in the bundled dir and attach descriptions to bundled extensions in help; `PATH`/custom extensions and bundled names missing from the file are name-only; a missing/unreadable file degrades to name-only without error; never execute an extension during help
- [ ] 5.6 Wire config initialization only where needed (extension dispatch needs config dir/endpoint); keep side-effect-free paths unaffected
- [ ] 5.7 Integration tests: built-in precedence, unknown→extension, unknown with no extension errors, help listing showing bundled descriptions from the file, PATH extensions name-only (and not executed during help), missing-descriptions-file degrades to name-only, and `lstk --non-interactive <ext>` conveying `LSTK_EXT_NON_INTERACTIVE` while not forwarding the flag

## 6. Reference extension and end-to-end coverage

- [ ] 6.1 Add a small reference/example `lstk-*` extension used by tests that echoes the `LSTK_EXT_*` it received
- [ ] 6.2 Integration test: place the reference extension on a test `PATH`, invoke via `lstk <name>`, and assert it received the expected runtime context (endpoint when emulator running, auth token when authed)

## 7. Bundled LocalStack extensions (distribution + update)

- [ ] 7.1 Define the bundled-extensions on-disk layout next to the lstk binary (including the descriptions file) and how each channel populates it: binary archive (sibling files), Homebrew (libexec, not symlinked to global bin), npm (package dir resolvable via the symlink-resolved exe path)
- [ ] 7.2 Generate the bundled descriptions file (name → one-liner) during the release process, version-locked to the bundled binaries, and ship it where lstk reads it
- [ ] 7.3 Wire the public release workflow to pull prebuilt closed-source bundled binaries (e.g. `lstk-deploy`) from private CI, version-pinned to the lstk release, without exposing source
- [ ] 7.4 Extend `internal/update` to replace lstk, its bundled extensions, and the descriptions file as one atomic, version-matched set across all install methods; never leave them mismatched on interrupted update
- [ ] 7.5 Add a reference bundled extension (in-tree, for tests) with a descriptions-file entry that echoes the `LSTK_EXT_*` it received and performs a stubbed self-authorization, exercising the bundled path end to end
- [ ] 7.6 Integration tests: bundled extension resolvable immediately, bundled wins over a same-named `PATH` extension, resolvable via a symlinked/shim `lstk`, bundled description shown in help, and bundled premium extension still self-authorizes

## 8. Documentation and finalize

- [ ] 8.1 Add an "Extensions" section to CLAUDE.md describing the mechanism, the `LSTK_EXT_*` contract (including `LSTK_EXT_NON_INTERACTIVE`), bundled-dir + PATH resolution, bundled-wins precedence, the release-generated descriptions file (bundled-only help text), and the self-authorization model (lstk passes the token; authorization is the extension's job)
- [ ] 8.2 Write a public extension-author guide: the manifest-free contract, runtime-context variables, global-flag conveyance via env, that help descriptions are bundled-only (custom/PATH extensions are name-only), how to authorize the user with the conveyed token, and the security note that authorization must not rely on lstk (which is open source); note the deferred signed-entitlement mechanism
- [ ] 8.3 Run `make lint`, `make test`, and `make test-integration`; ensure all pass
