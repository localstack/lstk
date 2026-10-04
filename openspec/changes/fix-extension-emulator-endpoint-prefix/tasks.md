# Tasks

## 1. Reproduce (red)

- [x] 1.1 Add `TestExtensionEmulatorEndpointPerType` to `test/integration/extension_test.go` (`requireDocker`, serial, `cleanup()` before and after). Start an AWS, a Snowflake and an Azure placeholder container (`startTestContainer`, `startTestSnowflakeContainer`, `startTestAzureContainer`), set `LOCALSTACK_HOST=localhost.localstack.cloud:4566`, and run the `ref` extension. Assert these three `EMULATOR=` lines: `aws http://localhost.localstack.cloud:4566`, `snowflake http://snowflake.localhost.localstack.cloud:4566`, `azure https://azure.localhost.localstack.cloud:4566`. Verify with `make test-integration RUN=TestExtensionEmulatorEndpointPerType` that it fails on the Snowflake/Azure lines for the expected reason (no prefix, `http`). If the placeholders can't run alongside each other (same internal port), split into one subtest per type, each starting a single container.
- [x] 1.2 Add `TestExtensionEmulatorEndpointIPHostHasNoSubdomain`: Snowflake and Azure placeholders with `LOCALSTACK_HOST=127.0.0.1:4566`, asserting `snowflake http://127.0.0.1:4566` and `azure https://127.0.0.1:4566`. Verify it fails on the Azure scheme before the fix.

## 2. Implement (green)

- [x] 2.1 Add `container.EmulatorURL(t config.EmulatorType, resolvedHost string) string` in `internal/container`, per design.md. Snowflake delegates to `snowflake.Hostname`; Azure uses the `azure` subdomain and `https`; an IP host drops the subdomain; AWS and unknown types give `http://<host>`. Check the import graph before choosing between `azureconfig.AzureSubdomain` and a local constant. Add a table-driven unit test covering each type with both a hostname and an IP host, and verify with `go test ./internal/container/ -run TestEmulatorURL`.
- [x] 2.2 In `resolveEmulators` (`cmd/extension.go`), replace `"http://" + host` with `container.EmulatorURL(c.Type, host)`, and update the `Emulator.Endpoint` field comment in `internal/extension/context.go` to say the value is per type. Verify that both tests from group 1 now pass and that `TestExtensionEndpointConveyedWhenEmulatorRunning` still passes.

## 3. Document

- [x] 3.1 In `docs/extensions-authoring.md`, add one line to the `emulators` row/paragraph giving the per-type endpoint format (Snowflake `http://snowflake.…`, Azure `https://azure.…`, no subdomain on an IP host) and telling extension authors not to add the prefix themselves. Verify the existing `jq` example still selects by `type`.

## 4. Integration check

- [x] 4.1 Run `make test`, `make lint`, and the extension integration tests (`make test-integration RUN=TestExtension`), and verify everything passes. Then run `openspec validate fix-extension-emulator-endpoint-prefix --strict` and verify it reports no errors. (Note: `TestExtensionSelfAuthorizationRefusesWithoutToken` fails identically on unmodified `main`, exit 1 vs expected 13; it is unrelated to this change.)
