## Context

`lstk` today is a single Go binary whose commands are wired in `cmd/` (Cobra) with domain logic in `internal/`. New capabilities must be merged into the open-source core, which blocks proprietary/partner features and couples their release to the core. We want to let internal and external authors add `lstk <name>` subcommands as independent binaries — open or closed source — placed on the user's `PATH`. lstk's job is dispatch plus handing the extension enough runtime context to be useful; any authorization decision belongs to the extension.

The closest existing pattern in the codebase is the IaC proxy (`internal/iac/<tool>/cli/`, wired via `cmd/iac.go`): lstk resolves an external binary, builds a child environment (resolved endpoint, credentials, stripped real-AWS vars), execs it with `DisableFlagParsing: true`, streams stdio, and propagates the exit code. The extension mechanism generalizes this pattern from a fixed set of known tools to a dynamic, user-installable set discovered by name on `PATH`.

Relevant existing building blocks:
- **Auth**: `internal/auth` resolves the token (keyring → `LOCALSTACK_AUTH_TOKEN` → file fallback). The token is conveyed to the extension; lstk does not validate licenses for extensions.
- **Endpoint resolution**: `internal/endpoint.ResolveHost()` and container discovery (`internal/container`) already produce the emulator URL used by `lstk aws`/IaC proxies.
- **Config dir**: `internal/config` resolves the lstk config directory; extension dispatch passes it through.

## Goals / Non-Goals

**Goals:**
- Git-style dispatch: `lstk <name>` → `lstk-<name>` executable on `PATH` when `<name>` is not built-in, with built-ins always winning.
- A stable, versioned environment-variable contract (`LSTK_EXT_*`) carrying emulator endpoint/type/port, config dir, and auth token.
- Convey the auth token so an extension can authorize the user itself; lstk makes no entitlement decision.
- Parse lstk's own global flags (e.g. `--non-interactive`) before the command name and convey the resolved state to the extension via the `LSTK_EXT_*` contract.
- Bundle LocalStack's own extensions (e.g. a closed-source `lstk-deploy`) alongside `lstk` so they ship by default, resolve ahead of `PATH`, and update atomically with lstk.
- Help listing (bundled dir + PATH scan) so installed extensions are discoverable by name.
- Keep the mechanism in the open-source repo; closed-source extensions ship only as binaries (bundled by LocalStack, or placed on `PATH` by anyone else).

**Non-Goals (deferred to future work):**
- lstk-side entitlement verification, signed grants / signed entitlement descriptions, and offline grant verification by extensions.
- Emulator genuineness attestation (licensed vs. clone).
- User-facing `lstk extension` management commands (`install`/`remove`/`list`/`info`) and a user-mutable managed extensions directory. (Static, ships-with-lstk bundling is in scope — see Decision 6 — but the user-driven management UX is not.)
- An in-process plugin ABI (Go plugins, shared libraries, WASM) — we deliberately use separate processes for language-agnostic, closed-source friendliness.
- Sandboxing or capability-limiting extension processes — extensions run with the user's full privileges, same as the IaC proxies and any tool on `PATH`.

## Decisions

### Decision 1: Separate-process, Git-style extensions (not in-process plugins)

`lstk <name>` resolves and execs an `lstk-<name>` binary on `PATH`, forwarding args/stdio/exit code, exactly like Git's `git-<name>` model and lstk's own IaC proxies.

**Rationale**: This is the only model that cleanly satisfies "open or closed source" and "internal or external authors" — a closed-source extension ships as an opaque binary in any language and never touches the core repo. Go's `plugin` package is Linux/macOS-only, requires identical toolchain/build flags, and exposes a Go ABI (excludes other languages and complicates closed source). WASM would sandbox nicely but can't easily run arbitrary partner toolchains and adds a heavy runtime. Process-per-invocation matches an existing, proven pattern in this codebase.

**Alternatives considered**: Go `plugin` shared objects (rejected: platform/toolchain coupling, Go-only); embedded scripting/WASM (rejected: language lock-in or runtime weight); MCP-style long-running server processes (rejected: overkill for CLI subcommands, more lifecycle complexity).

### Decision 2: Dispatch via Cobra's unknown-command hook, built-ins first

Wire extension dispatch in `cmd/root.go` so that resolution happens only after Cobra fails to match a built-in command/alias. This guarantees built-ins always shadow extensions (spec requirement) and avoids parsing extension flags (`DisableFlagParsing` semantics applied to the synthesized extension command, as the IaC proxies do).

**Rationale**: Reuses Cobra's existing matching and keeps the change localized. An extension can never override `start`, `snapshot`, etc., which matters because extensions are user-installable.

**Global flags before the command name**: lstk's global flags (`--non-interactive`, `--config`, and any added later) must be recognized when they precede the extension name but must not be parsed out of the extension's own args. The mechanism is `SetInterspersed(false)` on the root flag set: Cobra parses *leading* flags into `cfg` and stops at the first positional (the command name), handing the dispatch path everything from the command name onward verbatim. This gives Git-style "globals only before the command" for free and, because it reuses the existing persistent-flag definitions, any future global flag is handled with no extension-specific code. This deliberately differs from the IaC proxies, which strip `--non-interactive` from *any* position via `stripGlobalFlags` ([cmd/proxy.go](../../../cmd/proxy.go)); for generic extensions, stripping a flag out of the middle of the extension's args could clobber an extension's own identically-named flag, so we only consume leading globals. The proxy's manual `stripGlobalFlags` remains the fallback if `SetInterspersed(false)` interacts badly with the existing bare-root `start` behavior.

**Alternatives considered**: Pre-scanning `os.Args[1]` before Cobra runs (rejected: duplicates Cobra's alias/normalization logic and risks divergence). Registering a synthetic Cobra command per discovered extension at startup (we do a lightweight bundled-dir + PATH scan for `--help` listing but keep dispatch in the unknown-command path to avoid eagerly stat-ing the filesystem on every invocation). Reusing `stripGlobalFlags` verbatim (rejected as the primary path: its strip-from-anywhere behavior risks colliding with extension-owned flags).

### Decision 3: Environment-variable runtime contract (`LSTK_EXT_*`), versioned

Context flows to the extension purely through environment variables prefixed `LSTK_EXT_`, with `LSTK_EXT_API_VERSION` as an integer contract version. Variables in this change: `LSTK_EXT_API_VERSION`, `LSTK_EXT_EMULATOR_ENDPOINT`, `LSTK_EXT_EMULATOR_TYPE`, `LSTK_EXT_EMULATOR_PORT`, `LSTK_EXT_CONFIG_DIR`, `LSTK_EXT_AUTH_TOKEN`, and `LSTK_EXT_NON_INTERACTIVE`. The host environment is inherited; only `LSTK_EXT_*` is added/overridden. Endpoint vars are omitted when no emulator is running; the auth token is omitted when none is available.

Resolved global flags travel by env, not argv: `LSTK_EXT_NON_INTERACTIVE` is set when the session is non-interactive (the `--non-interactive` flag was given or stdout is not a TTY — the same condition lstk already computes at [cmd/root.go](../../../cmd/root.go) `isInteractive`). Carrying global state by env (rather than re-forwarding `--non-interactive` on the extension's command line) is what lets the extension own its entire flag space without collision, and each new global flag becomes one additive `LSTK_EXT_*` variable.

**Rationale**: Mirrors how the IaC proxies already pass context, is language-agnostic (every runtime can read env), and avoids forcing extensions to parse lstk's TOML or re-implement discovery. Versioning makes the contract evolvable: additive within a major version, bump on any removal/repurpose. A future signed-entitlement description would be added additively (e.g. `LSTK_EXT_GRANT` / `LSTK_EXT_PUBLIC_KEY`) under a documented version bump.

**Alternatives considered**: A JSON context file referenced by a single env var (cleaner for large/structured payloads; reconsider if the contract grows, but env is simpler for the current fields and avoids temp-file lifecycle/cleanup). Passing context as CLI flags (rejected: collides with extension-owned flag space).

### Decision 4: No lstk-side manifest — extensions are self-describing

lstk executes any resolvable `lstk-<name>` on `PATH` without reading a manifest. Contract compatibility is checked by the extension reading `LSTK_EXT_API_VERSION`; whether auth is needed is the extension's call (lstk always passes the token when it has one); a human description is shown in help only as the command name (PATH scan).

**Rationale**: This is the pure Git model (`git-<name>` extensions have no manifest) and it removes a whole class of questions — where the manifest lives in a shared `PATH` bindir, how lstk trusts it, how it stays in sync with the binary. Because lstk no longer makes any entitlement decision (Decision 5), it has no need to read an entitlement name from a manifest either.

**Alternatives considered**: A co-located `extension.toml` (rejected for now: ambiguous in shared `PATH` directories and unnecessary once lstk does no pre-launch gating). A managed extensions directory that owns manifests (deferred together with `lstk extension` management).

### Decision 5: Authorization belongs to the extension; the signed mechanism is deferred

lstk conveys `LSTK_EXT_AUTH_TOKEN` and makes no entitlement or license decision for an extension. An extension that wants to restrict its use authorizes the user itself — typically a server-side check against the LocalStack platform with the conveyed token.

**Rationale**: Because lstk is open source and rebuildable, any lstk-side gate is a UX speed bump, not a control — the only durable boundary is a check the extension performs against something a modified lstk cannot forge (a server response keyed to the user's token, or a signature the extension verifies). Deferring the signed mechanism lets the host land now with no platform changes; when signing is added, it slots into the `LSTK_EXT_*` contract additively without changing dispatch.

**Alternatives considered**: lstk-side entitlement gate + per-extension signed grant (the original design; deferred, not rejected — it improves UX and enables offline verification but requires platform work and adds complexity not needed for the first cut). Purely client-side license checks inside lstk (rejected: trivially bypassable, no IP protection).

### Decision 6: Bundled LocalStack extensions live next to the binary and resolve ahead of PATH

LocalStack ships its own extensions (e.g. a closed-source `lstk-deploy`) in a fixed directory derived from lstk's own symlink-resolved executable path — the Git `libexec/git-core` model. Resolution order becomes: built-ins → bundled dir → `PATH`, so a bundled extension wins over a same-named `PATH` executable. `internal/update` replaces lstk and its bundled extensions as one atomic, version-matched set. This reuses the `lstk-<name>` naming convention (no manifest) and does not change the authorization model — a bundled premium extension still self-authorizes with the conveyed token.

**Rationale**: Searching the directory next to the resolved executable is robust where co-located binaries are not on `PATH` — bare tarballs where the user symlinks only `lstk`, and npm/Homebrew layouts where the invoked `lstk` is a shim (hence resolving symlinks to find the real sibling dir). Bundled-wins precedence makes the official `lstk-deploy` deterministic: a stray or malicious `lstk-deploy` on `PATH` cannot hijack it. This is a deliberately narrow re-introduction of what the management capability deferred: a *static, read-only, ships-with-lstk* location and the packaging/update wiring — but **not** the user-mutable managed dir or the `lstk extension install/remove` UX, which stay deferred. The closed-source binaries are built in private CI and injected into the public release artifacts, version-pinned to the lstk release that carries them.

**Alternatives considered**: Just install bundled binaries onto `PATH` (rejected: zero lstk change but fragile for bare tarballs and pollutes the user's `PATH`). PATH-wins precedence (rejected: lets a stray binary shadow the official extension; bundled-wins is safer for closed-source premium commands). A managed mutable dir under the config dir (deferred with the rest of management; bundling needs only a static dir).

### Decision 7: One-line help descriptions from a release-generated file, bundled-only

Bundled extensions get a one-line description in `lstk help` from a static descriptions file generated during the release process and shipped alongside the bundled extensions (a single file mapping command name → description, e.g. `extensions.toml` in the bundled dir). lstk reads it at help time and never executes an extension to obtain text. `PATH`/custom extensions are always name-only.

**Rationale**: This keeps help rendering side-effect-free — the property we lost in the earlier exec-based approach. Because LocalStack controls the bundled set, their descriptions are known at release time, so there is no need to run anything: no timeout, no cache, no incidental-usage-on-a-typo hazard, no risk of executing untrusted `PATH` binaries during help. It is not a return of the per-extension `extension.toml` manifest (Decision 4): it is one release-owned file covering only the bundled set, generated by CI and version-locked to the binaries it describes, not authored per-extension by third parties. The cost — descriptions are static and only available for bundled extensions — is acceptable: third-party `PATH` extensions being name-only matches Git's `git help -a`.

**Alternatives considered**: A describe protocol that execs `lstk-<name> lstk:describe` (rejected: turns inert help into code execution, needs timeout/cache/trust-boundary machinery, and a mistyped command rendering Cobra usage could fork extensions). Per-extension manifests (rejected with Decision 4). Name-only for everything (viable and simplest, but the user wants descriptions for bundled extensions like `lstk-deploy`, which this delivers with no exec).

## Threat Model: hostile lstk rebuild (why authorization lives in the extension)

Because lstk is and remains open source, the adversary is assumed to have the full lstk source and can rebuild it with any check removed or any value forged. The single question every security claim must pass is: **does this check depend on lstk behaving honestly, or on a response the attacker cannot produce?** Anything in the first category is a UX speed bump, not a control.

Consequence for this change: lstk deliberately holds **no** authorization logic. It conveys the auth token and dispatches. An extension that needs protection must anchor its enforcement server-side (a LocalStack platform call keyed to the token) or in its own verification — a rebuilt lstk that strips or forges `LSTK_EXT_*` values cannot make such a check pass. This is also why the eventual signed-entitlement description (deferred) is designed to be verified *by the extension* against LocalStack's public key, not gated by lstk.

Residual risk we explicitly accept: an extension whose value is purely local logic cannot be protected client-side at all — an attacker rebuilds it with the checks removed. The only durable protection is server-side gating of the valuable behavior. This is the standard DRM reality and should be stated plainly in the author guide rather than implied away.

## Risks / Trade-offs

- **Auth token exposed to extension processes via env** → Tokens already flow to subprocesses for IaC proxies; only pass it when one is resolved, and document the trust boundary. A future scoped/short-lived token can replace the raw auth token without changing the contract shape.
- **Name collisions / malicious shadowing** → Built-ins always win (dispatch only on unknown commands); bundled LocalStack extensions win over `PATH`, so a stray `lstk-deploy` on `PATH` cannot hijack the official one. Among `PATH` entries, standard first-match-wins resolution applies, same as any `lstk-*` or shell command.
- **Bundled/lstk version skew** → `internal/update` must replace lstk and its bundled extensions atomically; an interrupted update must not leave them mismatched. Treated as a single versioned set.
- **npm/Homebrew shim hides the real binary location** → resolve symlinks to find lstk's real executable before locating the sibling bundled dir; verify on each channel.
- **Untrusted extension binaries run with user privileges** → Same trust model as installing any CLI tool or the IaC binaries lstk already shells out to; no sandboxing is promised.
- **No lstk-side authorization in this cut** → An extension that forgets to authorize is wide open; the author guide must make the self-authorization responsibility explicit and provide a recommended pattern (server-side check with the conveyed token).
- **Contract drift between lstk and extensions** → `LSTK_EXT_API_VERSION` lets extensions detect/require a minimum contract and refuse to run when incompatible.

## Migration Plan

- Purely additive: no built-in command behavior changes, so no user migration is required. Existing `terraform-proxy`/`cdk-proxy` specs are untouched.
- Land the host mechanism (dispatch + runtime context) with a reference extension so discovery, dispatch, the `LSTK_EXT_*` contract, and help listing are testable with no platform dependency.
- Rollback: because dispatch only triggers on unknown commands, having no `lstk-*` on `PATH` leaves built-in behavior identical.

## Open Questions

- **Token scoping**: Should extensions receive the raw `LOCALSTACK_AUTH_TOKEN` or a derived, audience-/extension-scoped, short-lived token? (Affects the eventual signed mechanism too.)
- **Help-listing cost**: Scanning the bundled dir + all of `PATH` for `lstk-*` on every `--help` has a filesystem cost; is lazy/cached scanning warranted, or is on-demand fine?
- **Closed-source build/release pipeline**: Where do the prebuilt closed-source bundled binaries (e.g. `lstk-deploy`) come from at release time — a private artifact registry, a private release? How are their versions pinned to the lstk release, and how does the public repo's release workflow pull them into the Homebrew formula, npm package, and binary archive without exposing source?
- **Exact bundled-dir layout**: Is it the same directory as the lstk binary, or a dedicated sibling (e.g. `libexec`-style) to avoid mixing with unrelated binaries — and how does each channel (Homebrew Cellar/libexec, npm package dir, tarball root) lay it out so the symlink-resolved lookup finds it?
- **Future signed entitlement**: When the deferred mechanism lands, what is the token format (JWT? which alg/curve), how is LocalStack's public key distributed to extensions, and what is the key-rotation strategy? (Tracked for the follow-up change, not this one.)
