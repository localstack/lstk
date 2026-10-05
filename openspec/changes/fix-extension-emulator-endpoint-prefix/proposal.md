# Proposal

## Why

The `emulators[].endpoint` field that lstk passes to extensions in `LSTK_EXT_CONTEXT` is always `http://<resolved host>`, e.g. `http://localhost.localstack.cloud:4566`. That is right for AWS, but wrong for Snowflake and Azure. Those emulators are served under the `snowflake.` and `azure.` subdomains, and Azure is served over `https://`. An extension that trusts the field ends up talking to the wrong virtual host (DEVX-1160).

The spec already says the endpoint is "resolved with the same discovery and host resolution used by built-in commands". The built-ins (`lstk start`/`status` for Snowflake, `lstk az`/`setup azure` for Azure) do add the prefix, so extension dispatch is the only place that doesn't.

## What Changes

- For a running Snowflake emulator, `emulators[].endpoint` becomes `http://snowflake.<host>:<port>`. This is the same URL `lstk start` prints as "Snowflake endpoint".
- For a running Azure emulator, `emulators[].endpoint` becomes `https://azure.<host>:<port>`. This is the same URL `lstk az` and `lstk setup azure` target.
- AWS stays `http://<host>:<port>`, unchanged.
- When the resolved host is an IP address (DNS check failed, or `LOCALSTACK_HOST` set to an IP), no subdomain is added, because a subdomain of an IP isn't a valid host. The scheme still follows the emulator type.
- `port` and `type` are unchanged. `LSTK_EXT_API_VERSION` is not bumped: no field is removed or repurposed, and the field now matches its existing contract.
- The extension authoring docs gain a per-type endpoint example.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `extension-runtime-context`: the "Running emulators are provided as a JSON array" requirement now states the per-type endpoint format (subdomain and scheme) and the IP fallback, with scenarios for Snowflake, Azure, and the IP-host case.

## Impact

- **Code:** `resolveEmulators` in `cmd/extension.go`, which currently hardcodes `"http://" + host`, and a new domain helper that builds an emulator's URL from its type and resolved host. The helper reuses `snowflake.Hostname` and the Azure subdomain/scheme from `internal/azureconfig`.
- **Extensions:** extensions that read the Snowflake or Azure `endpoint` get a working URL. An extension that worked around the bug by adding the prefix itself would now double it. No such extension is known; the bundled extensions select AWS.
- **Tests:** a new integration test in `test/integration/extension_test.go` using the existing Snowflake/Azure placeholder containers.
- **Docs:** `docs/extensions-authoring.md`.
