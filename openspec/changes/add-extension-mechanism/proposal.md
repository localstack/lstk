## Why

`lstk` ships a fixed set of built-in commands, so any new capability — whether built by LocalStack engineers or partners — has to land in the core open-source repository. That blocks closed-source/proprietary features, slows third-party contribution, and couples every new feature's release to the core CLI's release cadence. We want a Git-style extension mechanism so anyone can add `lstk <name>` subcommands as separate binaries on their `PATH`, with lstk handing each extension enough runtime context (resolved emulator endpoint, config dir, auth token) to do useful work — while leaving any authorization decision to the extension itself.

## What Changes

- Introduce a Git-style extension model: when `lstk <name>` is not a built-in command, lstk resolves and executes an `lstk-<name>` executable found on `PATH`, forwarding all remaining arguments and propagating the child's stdin/stdout/stderr and exit code.
- Define an **extension runtime contract**: lstk passes the resolved emulator endpoint, emulator type, config directory, auth token, and resolved global-flag state to the extension process through a stable, versioned set of `LSTK_EXT_*` environment variables so extensions can talk to the emulator and the platform without re-implementing discovery or config resolution.
- **Honor global flags before the command name**: lstk parses its own global flags (e.g. `--non-interactive`, and any added later) when they precede the extension name, consumes them itself, and conveys the resolved state to the extension via `LSTK_EXT_*` (e.g. `LSTK_EXT_NON_INTERACTIVE`) rather than forwarding them on the extension's command line.
- **Bundle LocalStack's own extensions**: ship LocalStack-built extensions (e.g. a closed-source `lstk-deploy`) by default alongside `lstk` in a directory next to the binary, resolved ahead of `PATH` and updated atomically with `lstk` — with no user-facing install step.
- Establish that **authorization is the extension's responsibility**: lstk conveys the user's auth token and makes no entitlement decision of its own. An extension that needs to restrict its use authorizes the user itself (e.g. by calling the LocalStack platform with the conveyed token). A richer lstk-side mechanism — lstk obtaining a LocalStack-signed entitlement description for the extension to verify offline — is deliberately **deferred** to future work.
- List resolvable extensions in `lstk help` by scanning the bundled directory and `PATH` for `lstk-*` executables; bundled extensions show a one-line description read from a static descriptions file generated during the release process, while `PATH`/custom extensions are name-only. Help rendering never executes an extension.
- Keep the entire mechanism in the open-source repository; closed-source extensions ship only as binaries placed on `PATH` and never require source in the core repo.

## Capabilities

### New Capabilities

- `extension-framework`: Git-style discovery, resolution, and dispatch of `lstk-<name>` extension executables (bundled dir + `PATH`), including built-in precedence, leading-only global-flag handling, forwarding of arguments/streams/exit codes, and side-effect-free help listing (bundled extensions described from a static file, others name-only). No lstk-side manifest — extensions are self-describing and self-validating.
- `extension-runtime-context`: The versioned environment-variable contract lstk establishes for an extension process — resolved emulator endpoint/type/port, config directory, auth token, and resolved global-flag state (e.g. non-interactive) — so extensions can reach the emulator and platform and honor lstk's global flags.
- `extension-entitlement`: The authorization model — lstk conveys the auth token and the extension authorizes itself — plus the explicit deferral of any lstk-side signed-entitlement mechanism and the security rationale (lstk is open source, so authorization cannot depend on it).
- `extension-bundling`: Shipping LocalStack-built (possibly closed-source) extensions by default alongside `lstk` — a read-only bundled directory next to the binary, resolution ahead of `PATH`, a release-generated descriptions file for help text, cross-channel packaging (binary archive, Homebrew, npm), and atomic version-matched updates via `internal/update`. Excludes user-facing management commands and a user-mutable directory.

### Modified Capabilities

<!-- No existing capability's requirements change; the IaC proxy specs (terraform-proxy, cdk-proxy) are unaffected. -->

## Impact

- **New code**: `internal/extension/` (bundled-dir + PATH resolution, runtime context builder, global-flag conveyance, exec), and unknown-command dispatch + help-listing wiring in `cmd/root.go`.
- **Touched code**: `cmd/root.go` (fallthrough to extension dispatch for unknown commands; `SetInterspersed(false)` for leading-only global flags; bundled-dir + PATH scan for help), reuse of `internal/auth` (token resolution), `internal/config`/`internal/endpoint` (config dir and emulator endpoint resolution), `internal/container` (running-emulator discovery for endpoint/type), and `internal/update` (atomic version-matched update of bundled extensions).
- **Packaging/release**: binary archive, Homebrew formula, and npm package must lay out bundled extensions where lstk resolves them; the public release workflow must pull prebuilt closed-source bundled binaries from private CI, version-pinned to the lstk release.
- **External dependencies/services**: None required by this change. Extensions that authorize use the existing LocalStack platform with the conveyed auth token; no new platform or emulator endpoints are needed.
- **Security surface**: lstk passes the auth token into extension processes via env (as it already does for IaC proxies); this defines a local trust boundary to document. Authorization guarantees live in the extension, never in lstk.
- **Docs**: New "Extensions" section in CLAUDE.md and a public extension-author guide (manifest-free contract, `LSTK_EXT_*` variables, the self-authorization model and why it cannot rely on lstk).

## Deferred (future work)

- lstk-side entitlement verification and a LocalStack-signed entitlement description (grant) that extensions verify offline against a published public key.
- Emulator genuineness attestation (distinguishing a licensed emulator from a clone).
- User-facing `lstk extension` management commands (`list`/`info`/`install`/`remove`) and a user-mutable managed extensions directory. (Static, ships-with-lstk bundling is **in scope** via the `extension-bundling` capability; only the user-driven install/remove UX is deferred.)
