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

### Decision 3: Environment-variable runtime contract — a versioned JSON context object

Context flows to the extension through exactly two environment variables:

- `LSTK_EXT_API_VERSION` — a flat integer contract version, kept outside the payload so an extension can check compatibility *before* parsing anything.
- `LSTK_EXT_CONTEXT` — a single JSON object carrying the whole resolved context:
  - `configDir` (string, always present)
  - `authToken` (string, **omitted** when no token is available)
  - `nonInteractive` (bool)
  - `emulators` (array of `{ "type", "endpoint", "port" }`, **`[]` when no emulator is running**)

The host environment is inherited; only these two `LSTK_EXT_*` variables are added/overridden (any stray `LSTK_EXT_*` in the user's shell is stripped first so it cannot shadow the resolved values).

Resolved global flags travel inside this object, not on argv: `nonInteractive` is true when the session is non-interactive (the `--non-interactive` flag was given or stdout is not a TTY — the same condition lstk computes at [cmd/root.go](../../../cmd/root.go) `isInteractive`). Carrying global state in the context (rather than re-forwarding `--non-interactive` on the extension's command line) lets the extension own its entire flag space without collision, and each new global flag becomes one additive JSON field.

**Rationale**: The decisive driver is **multi-emulator support** (PR review): lstk can run an AWS, a Snowflake, and an Azure emulator simultaneously, which the original flat `LSTK_EXT_EMULATOR_ENDPOINT/TYPE/PORT` triple cannot represent. Adopting a JSON `emulators` array **from day one** means multi-emulator is expressible without a later breaking change to the contract. Once one field needs structure, a single structured object beats a growing sprawl of flat scalars: every future field is an additive JSON key under the same version rather than a new variable name, and the payload is still language-agnostic (every runtime has a JSON parser) and never forces the extension to read lstk's TOML or re-implement discovery. Keeping `LSTK_EXT_API_VERSION` as a flat scalar preserves clean version negotiation. A future signed-entitlement description slots in as an additive field (e.g. `grant` / `publicKey`) under a documented version bump.

**Alternatives considered**: Flat per-field variables — the original design; rejected because it cannot represent multiple running emulators and grows a new variable per field. A JSON context *file* referenced by a path env var (rejected: temp-file lifecycle/cleanup, where inline JSON in the env var is simpler and the payload is small). Putting the version *inside* the JSON too (rejected: keeping it a flat scalar lets an extension negotiate compatibility before parsing). Passing context as CLI flags (rejected: collides with extension-owned flag space).

### Decision 4: No lstk-side manifest — extensions are self-describing

lstk executes any resolvable `lstk-<name>` on `PATH` without reading a manifest. Contract compatibility is checked by the extension reading `LSTK_EXT_API_VERSION`; whether auth is needed is the extension's call (lstk always passes the token when it has one); a human description is shown in help only as the command name (PATH scan).

**Rationale**: This is the pure Git model (`git-<name>` extensions have no manifest) and it removes a whole class of questions — where the manifest lives in a shared `PATH` bindir, how lstk trusts it, how it stays in sync with the binary. Because lstk no longer makes any entitlement decision (Decision 5), it has no need to read an entitlement name from a manifest either.

**Alternatives considered**: A co-located `extension.toml` (rejected for now: ambiguous in shared `PATH` directories and unnecessary once lstk does no pre-launch gating). A managed extensions directory that owns manifests (deferred together with `lstk extension` management).

### Decision 5: Authorization belongs to the extension; the signed mechanism is deferred

lstk conveys `LSTK_EXT_AUTH_TOKEN` and makes no entitlement or license decision for an extension. An extension that wants to restrict its use authorizes the user itself — typically a server-side check against the LocalStack platform with the conveyed token.

**Rationale**: Because lstk is open source and rebuildable, any lstk-side gate is a UX speed bump, not a control — the only durable boundary is a check the extension performs against something a modified lstk cannot forge (a server response keyed to the user's token, or a signature the extension verifies). Deferring the signed mechanism lets the host land now with no platform changes; when signing is added, it slots into the `LSTK_EXT_*` contract additively without changing dispatch.

**Alternatives considered**: lstk-side entitlement gate + per-extension signed grant (the original design; deferred, not rejected — it improves UX and enables offline verification but requires platform work and adds complexity not needed for the first cut). Purely client-side license checks inside lstk (rejected: trivially bypassable, no IP protection).

### Decision 6: Bundled extensions resolve from a directory next to the binary, ahead of PATH (distribution + update deferred)

lstk resolves bundled extensions (e.g. a closed-source `lstk-deploy`) from a fixed directory derived from its own symlink-resolved executable path — the Git `libexec/git-core` model. Resolution order is: built-ins → bundled dir → `PATH`, so a bundled extension wins over a same-named `PATH` executable. This reuses the `lstk-<name>` naming convention (no manifest) and does not change the authorization model — a bundled premium extension still self-authorizes with the conveyed token.

**Scope split for the first release** (per PR review): the first release ships the *resolution* above — enough that a LocalStack-built `lstk-<name>` placed next to `lstk` runs as `lstk <name>` — so bundled extensions can be validated by **manual placement** before they are ever shipped automatically. What is **deferred to a separate future change (`add-bundled-extension-distribution`)**: packaging the bundled binaries into the binary-archive / Homebrew / npm payloads, pulling the closed-source binaries from private CI, shipping + release-validating the descriptions file, and the atomic version-matched co-update of the `lstk`/`lstk-*` set in `internal/update`. The first release deliberately does **not** download or upgrade bundled extensions; it is a test bed.

**Rationale**: Searching the directory next to the resolved executable is robust where co-located binaries are not on `PATH` — bare tarballs where the user symlinks only `lstk`, and npm/Homebrew layouts where the invoked `lstk` is a shim (hence resolving symlinks to find the real sibling dir). Bundled-wins precedence makes the official `lstk-deploy` deterministic: a stray or malicious `lstk-deploy` on `PATH` cannot hijack it. Shipping resolution first, distribution later, lets us de-risk the closed-source bundling pipeline (private-CI pull, atomic update, packaging across three channels) against a manually-staged binary before automating it — without blocking the run-extensions mechanism on that pipeline.

**Alternatives considered**: Just install bundled binaries onto `PATH` (rejected: zero lstk change but fragile for bare tarballs and pollutes the user's `PATH`). PATH-wins precedence (rejected: lets a stray binary shadow the official extension; bundled-wins is safer for closed-source premium commands). Shipping distribution + atomic update in the first release together with resolution (rejected per PR review: couples the first launch to the full closed-source pipeline; phasing it lets the first release validate bundled extensions manually). A managed mutable dir under the config dir (deferred with the rest of management; bundling needs only a static dir).

### Decision 7: One-line help descriptions from a hand-authored file owned by the private repo, bundled-only

Bundled extensions get a one-line description in `lstk help` from a static descriptions file shipped alongside the bundled extensions (a single file mapping command name → description, `lstk-extensions.toml` in the bundled dir). lstk reads it at help time and never executes an extension to obtain text. `PATH`/custom extensions are always name-only.

The file is **hand-authored and owned by LocalStack's private extensions repository** — the same place the closed-source bundled binaries are built — and the open-source lstk repo simply assumes it exists in the bundled dir. lstk does **not** generate it. The only safety rail kept in this repo is a release-time **validation** step, `scripts/check-descriptions.sh` (a bash script, consistent with the repo's only other script, `scripts/test-integration.sh`): it extracts the described command names from the file (the bare left-hand identifiers of the flat `name = "…"` table — it never needs to parse the description values) and fails the release if any described name has no corresponding `lstk-<name>` executable in the staged bundled dir. A shipped binary with no description is allowed and degrades to name-only.

**Validation targets a single, host-native staging dir.** The descriptions file is os/arch-independent — `deploy` is `deploy` on every platform — so the check runs once against one staging dir (the release host's own OS, i.e. Linux), where binaries are bare `lstk-<name>` with no `.exe` suffix or Windows executable-bit ambiguity. This keeps the shell check trivially correct (`find … -perm -u+x` + name-prefix strip) and avoids the per-OS `.exe`/PATHEXT handling that lstk's runtime `scanDir` only applies when `GOOS == windows`; pointing any validator at a Windows staging dir from a Linux host would otherwise mis-read `lstk-deploy.exe` as the name `deploy.exe` and false-fail.

**Rationale**: A shell script keeps the open-source repo's release tooling consistent (one `scripts/` home, no extra top-level `release/` dir for a single tool) and the logic is small and key-only. The validation needs only the described *names*, so there is no TOML value/quote parsing to get wrong, and validating one clean host-native dir removes the only real cross-platform hazard. This preserves the version-locking guarantee the former generator (`gen-descriptions` + `BuildDescriptions` + the `release/bundled-extensions.toml` manifest) enforced by construction, now enforced by the release-blocking check instead.

**Alternatives considered**: A Go validator in-repo (`extension.ValidateDescriptions` reusing the runtime `scanDir` + go-toml, run via `go run ./...`) — rejected: it is the only thing that *cannot* drift from how lstk resolves extensions at runtime, but it costs an extra build entrypoint, runs against the repo's "domain logic in Go, helpers in shell" grain for what is a 20-line set-difference, and the drift it guards against is negligible here (validation reads only bare-identifier keys against a single clean dir). Acceptable to lose that guarantee while the descriptions file stays a flat name→string table; revisit if the file gains structure (per-extension metadata, min API version) that should share types with `LoadDescriptions`. Generating the file from a manifest in the open-source repo (the original design; rejected: the descriptions belong with the binaries in the private repo, and generation duplicated a list the private repo already owns — validation gives the same version-lock guarantee with one source of truth). A describe protocol that execs `lstk-<name> lstk:describe` (rejected: turns inert help into code execution, needs timeout/cache/trust-boundary machinery, and a mistyped command rendering Cobra usage could fork extensions). Per-extension manifests (rejected with Decision 4). Name-only for everything (viable and simplest, but the user wants descriptions for bundled extensions like `lstk-deploy`, which this delivers with no exec).

### Decision 8: lstk records extension invocations as telemetry; trace propagation is deferred

When telemetry is enabled (lstk's existing `LSTK_OTEL` path), lstk SHALL record each extension invocation — the command name, duration, and exit code — through the same OpenTelemetry export the rest of lstk uses. This is **lstk-side only**: lstk does **not** inject trace context (W3C `traceparent`/`tracestate`) into the extension process, so an extension's own spans do not yet nest under lstk's trace.

**Rationale** (PR review): visibility into which extensions run, how often, and whether they succeed is valuable and cheap — lstk already has an OTEL pipeline, so the invocation is just one more span/event at the dispatch boundary, with no contract surface and no dependency on the extension being instrumented. Full distributed tracing *through* the extension is a larger, optional concern: it only helps extensions that are themselves OTEL-instrumented, and it is purely additive later (inject `traceparent`, or add a `trace` field to `LSTK_EXT_CONTEXT`, under a version bump) — so it is deferred rather than designed now. Recording respects the same opt-in/opt-out as all lstk telemetry; nothing is emitted when telemetry is disabled.

**Alternatives considered**: Inject standard `TRACEPARENT`/`TRACESTATE` so any instrumented extension auto-continues the trace (deferred, not rejected — clean and standard, but only useful once we have instrumented extensions, and addable without breaking the contract). An `LSTK_EXT_`-prefixed trace variable (rejected for now: non-standard, and the standard names are the natural choice when we do add propagation). No telemetry at all (rejected: the reviewer specifically wants extension observability).

### Decision 9: No shared TUI/library between lstk and extensions

Extensions do **not** link or share any lstk Go library — UI components, output sinks, or otherwise. The only coupling is the `LSTK_EXT_*` environment contract (Decision 3). An extension that wants a TUI, spinners, or styled output brings its own libraries.

**Rationale** (PR review): a shared library would bind extension authors to lstk's Go API surface (Bubble Tea, lipgloss, the `output`/`ui` packages), which changes without much external control and would couple every extension's build to lstk's dependency graph — exactly the lock-in the separate-process model (Decision 1) exists to avoid. A narrow, versioned env contract is far less likely to churn than a Go API, and keeps extensions language-agnostic and independently buildable. The cost — each extension re-implements its own presentation — is acceptable and is the same trade Git's extension model makes.

**Alternatives considered**: A published `lstk-extension-sdk` Go module with shared UI/output helpers (rejected for now: convenient for Go authors but Go-only, version-coupled, and contradicts the language-agnostic goal; could ship later as an *optional* convenience without changing the contract).

### Decision 10: No extension allow-list / trust policing

lstk does not maintain an allow-list, signature check, or any other trust gate over which `lstk-<name>` executables it will run. It resolves and execs whatever is on `PATH` or in the bundled dir, exactly as Git runs `git-<name>`.

**Rationale** (PR review): the only way to introduce a malicious `lstk-<name>` is to place an executable on the user's machine — at which point the attacker would target far higher-value binaries (`ls`, `cat`, the user's shell rc) rather than a `lstk-` prefixed one. A whitelist in open-source lstk is also trivially bypassable (Decision 5's threat model: lstk can be rebuilt), so it would be security theater. Bundled LocalStack extensions are not a separate download — they ship inside the `lstk` artifact (Decision 6) and inherit its trust — and lstk deliberately does **not** download extensions from the internet, which is where an allow-list would actually matter. Bundled-wins precedence (Decision 6) already prevents a stray `PATH` binary from shadowing an official bundled command, which is the one hijack worth blocking.

**Alternatives considered**: An `lstk extension add`/allow-list UX seen in some tools (rejected as overkill: it presumes internet-downloaded extensions, which is out of scope and deferred with the rest of management). Signature verification of extension binaries (rejected: meaningful only with a download channel and a trust root lstk cannot anchor while open source).

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
