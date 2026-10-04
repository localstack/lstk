# Design

## Context

Today, per-type endpoint knowledge lives in two places:

- `snowflake.Hostname(resolvedHost)` (`internal/emulator/snowflake`) returns `snowflake.<host:port>`, or `""` for an IP host. It is used by `lstk start` (post-start pointer) and `lstk status`, and both add `http://` themselves.
- `azureconfig.BuildEndpoint(host)` (`internal/azureconfig`) returns `https://azure.<host:port>` with no IP guard. Its callers (`lstk az`, `lstk setup azure`) hard-fail before reaching it when DNS doesn't resolve.

`resolveEmulators` in `cmd/extension.go` uses neither. It emits `"http://" + host` for every type.

## Goals / Non-Goals

**Goals:**
- One function that turns `(emulator type, resolved host:port)` into the emulator's URL, so extension dispatch can't drift from the built-ins again.

**Non-Goals:**
- Changing what `lstk start`, `lstk status`, `lstk az` or `lstk setup azure` print or target. They may adopt the helper later; this change doesn't touch their output.
- The `endpointUrl` field (conveyed verbatim, no per-type logic) and the `--endpoint-url` IP gap in `cmd/az.go`. Both are out of scope.
- Probing that the URL is reachable. Extension dispatch stays best-effort and never blocks.

## Decisions

**Put the helper in `internal/container` as `EmulatorURL(t config.EmulatorType, resolvedHost string) string`.**
`internal/container` already imports `config`, `endpoint` and `emulator/snowflake`, and it owns emulator discovery, which is what `resolveEmulators` calls. Alternatives considered:
- `internal/endpoint`: rejected. `emulator/snowflake` imports `endpoint`, so `endpoint` calling `snowflake.Hostname` would create an import cycle.
- A `switch` inline in `cmd/extension.go`: rejected. That is business logic at the command boundary (against CLAUDE.md), and it is exactly the kind of duplicate that let this bug happen.
- Importing `internal/azureconfig` into `container`: rejected. It would pull the `az` CLI wrapper into the start path just for a string constant. Instead, the helper reuses `azureconfig.AzureSubdomain` only if that adds no heavy dependency. Otherwise it keeps a local `"azure"` constant with a comment pointing at `azureconfig.BuildEndpoint`. The implementer picks after checking the import graph. Either way the observable URL is identical.

**IP host: skip the subdomain, keep the type's scheme.**
This matches `snowflake.Hostname`'s existing `""`-on-IP contract, which `status`/`start` already treat as "show the bare endpoint". Hard-failing the way `lstk az` does isn't an option, because extension dispatch must not fail on emulator-context resolution. For Azure, `https://127.0.0.1:4566` won't pass TLS hostname verification against the emulator's certificate, but it is still more truthful about the protocol than `http://`. An extension that needs Azure has no working URL in that environment anyway, and the spec records this fallback explicitly.

**Tests pin the resolved host with `LOCALSTACK_HOST`.**
`endpoint.ResolveHost` returns an override as-is, so setting `LOCALSTACK_HOST=localhost.localstack.cloud:4566` (or `127.0.0.1:4566`) makes the expected URL independent of whether the CI host's DNS resolves `localhost.localstack.cloud`. The Snowflake/Azure placeholder containers (`startTestSnowflakeContainer` / `startTestAzureContainer`) and the reference extension's `EMULATOR=<type> <endpoint> <port>` lines already provide everything else.

## Risks / Trade-offs

- An extension that already adds the prefix itself would end up with `snowflake.snowflake.…`. → None is known; the bundled extensions select `aws`. The authoring docs will state the per-type format.
- The unit and the built-ins could still diverge, because `status`/`start` keep calling `snowflake.Hostname` directly. → The helper delegates to `snowflake.Hostname`, so the Snowflake rule has a single source. Migrating the built-ins is a follow-up.
