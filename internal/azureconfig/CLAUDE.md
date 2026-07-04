# Azure CLI Integration Reference (internal/azureconfig)

Detail moved out of the root CLAUDE.md; see the root file for the command list.

## Isolated mode (`lstk setup azure` + `lstk az <args>`)

`lstk setup azure` (alias `lstk setup az`, matching the `lstk az` proxy command) prepares an isolated Azure CLI config dir (under the lstk config dir, via `AZURE_CONFIG_DIR`): registers a custom Azure cloud (`LocalStack`) whose endpoints point at the LocalStack Azure emulator, activates it, disables Azure CLI instance discovery and telemetry, and performs a one-time dummy service-principal login. The user's global `~/.azure` is left untouched. Requires the `az` CLI and a running Azure emulator.

`lstk az <args>` runs `az <args>` against that isolated config dir, so the Azure CLI talks to LocalStack for Azure service URLs and to the real internet for everything else (extension downloads, etc.).

The default `lstk az <args>` mode mirrors `lstk aws`: the Azure CLI has no `--endpoint-url`/`--profile`, so the only isolation knob is `AZURE_CONFIG_DIR`. Inside that isolated dir we register a custom cloud whose endpoints point at `https://azure.localhost.localstack.cloud:4566`, so `az` makes direct calls to LocalStack for Azure services (no HTTP(S) forward proxy in front of `az`). `core.instance_discovery=false` is required because `az` does not recognise the LocalStack host as a real Azure cloud. Adding a new Azure service that needs its own endpoint in `az`'s cloud config means extending the map in `internal/azureconfig/azureconfig.go::BuildCloudConfig`.

## Interception mode (`lstk az start-interception` / `stop-interception`)

Opt-in second mode: instead of the isolated dir, these mutate the user's **global** `~/.azure` so plain `az` (any terminal/script) targets LocalStack, then switch back. `start-interception` runs the same register → activate → `instance_discovery=false` → dummy-login flow against the global config (but does not touch global telemetry/survey prefs) and is independent of `lstk setup azure`. `stop-interception` switches the active cloud back to `AzureCloud` (override with `--cloud <name>`, validated against the live `az cloud list`) and re-enables instance discovery — but only if `LocalStack` is still the active cloud, to avoid clobbering an unrelated selection.

This offers azlocal's global pattern (the same cloud registration applied to `~/.azure` rather than the isolated dir), so existing `az` scripts run unmodified against LocalStack. It is intentionally documented as optional because it mutates global state; prefer the isolated `lstk az <args>` mode unless a script must invoke plain `az`. The interception domain logic lives in `internal/azureconfig/interception.go` and reuses the shared `registerLocalStackCloud` helper; the command wiring (subcommands under `az` plus the shared `azPreflight` checks) is in `cmd/az.go`.
