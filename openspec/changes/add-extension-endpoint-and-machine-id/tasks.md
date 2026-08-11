## 1. Expose the machine id

- [x] 1.1 Add a `MachineID(ctx)` accessor to `telemetry.Client` (`internal/telemetry/client.go`) that owns the lazy derivation via the client's `sync.Once` and memoizes it for the process; `GetEnvironment` reads through it, so the conveyed value and the one stamped on lstk's events are identical by construction. Document that it is the prepared hash, never a raw id
- [x] 1.2 Put the `enabled` guard *before* the computation and bound the derivation with `machineIDTimeout` (3s). The guard order is the contract: a disabled client must not dial Docker or persist a `machine_id` file to derive a value it would only mask. The timeout is the latency bound: the derivation's Docker `Info` dial runs pre-exec on the dispatch path with a deadline-free context, and without the bound a black-holed `DOCKER_HOST` (dead VPN route, stale remote context) stalls `lstk <ext>` for the OS TCP connect timeout — ~75s on macOS, over two minutes on Linux — before the extension runs; on expiry the derivation falls through to `sys_`/`gen_` like any other Docker failure (rationale in design.md, Decision 2)
- [x] 1.3 Unit-test that the first accessor call derives the id, that it equals the id on `GetEnvironment` and on emitted events, and that a disabled client never runs the derivation at all — asserted on the unexported field, not just the accessor, so a masked-but-computed value fails the test (`internal/telemetry/client_test.go`)

## 2. Add and populate the contract fields

- [x] 2.1 Add `MachineID string \`json:"machineId,omitempty"\`` and `EndpointURL string \`json:"endpointUrl,omitempty"\`` to `extension.Context` (`internal/extension/context.go`); `omitempty` is required, not stylistic — post-v1 fields are detected by presence. Extend the type's doc comment with what each field is, when it is omitted, and that `EndpointURL` is unvalidated and independent of `Emulators`
- [x] 2.2 Resolve the endpoint source at the extension-dispatch branch in `cmd/root.go` with `endpoint.ResolvedSource` — that scope holds the `*cobra.Command` the flag lives on, which `dispatchExtension` does not receive — and pass the value in; no error path, since nothing is validated
- [x] 2.3 Use `ResolvedSource`, not `Resolve`: the latter probes and hard-fails, which would make an endpoint-diagnosing extension unable to run against the endpoint it is meant to diagnose
- [x] 2.4 Populate both fields in `dispatchExtension` (`cmd/extension.go`) with `tel.MachineID(ctx)` directly — the accessor derives on first use, so dispatch needs no priming call, and the `EmitCommand` below reads the same memoized value
- [x] 2.5 Leave `resolveEmulators` and `extension.APIVersion` untouched: `emulators` answers a different question, and adding fields is additive
- [x] 2.6 Unit-test the round-trip and the omission of both keys when the values are empty (`internal/extension/extension_test.go`)

## 3. Make the fields observable end to end

- [x] 3.1 Mirror both fields in the reference extension's context struct and echo them as `MACHINE_ID=` / `ENDPOINT_URL=`, omitted when empty like `SESSION_ID` (`test/integration/test-samples/extensions/lstk-ref/main.go`) — without this the integration tests cannot observe the fields
- [x] 3.2 Integration test: the conveyed `machineId` equals `payload.environment.machine_id` on the `lstk_command` event lstk emits for the same invocation (mock analytics server), not merely that it is non-empty
- [x] 3.3 Integration test: with `LOCALSTACK_DISABLE_EVENTS=1` neither `machineId` nor `sessionId` is conveyed, and the extension still runs
- [x] 3.4 Integration test: `endpointUrl` is conveyed from the flag, from `LSTK_ENDPOINT_URL`, and from `AWS_ENDPOINT_URL`, with the documented precedence between them; the flag case must place the flag *before* the extension name, since anything after it is forwarded to the extension verbatim
- [x] 3.5 Integration test: a deliberately malformed value is echoed back byte for byte with a zero exit — this is what pins the pass-through guarantee, including the trailing slash `endpoint.Resolve` would have stripped
- [x] 3.6 Integration test: with no endpoint source set the field is absent (strip inherited `LSTK_ENDPOINT_URL`/`AWS_ENDPOINT_URL` so "no source" is genuine, since CI may set them)

## 4. Documentation

- [x] 4.1 `docs/extensions-authoring.md`: add both fields to the JSON sample and the field table
- [x] 4.2 `docs/extensions-authoring.md`: add a "Targeting an external emulator" section stating that the value is unvalidated and must be parsed by the extension, and how `endpointUrl` differs from `emulators`
- [x] 4.3 `docs/extensions-authoring.md`: fold `machineId` into the existing "Correlating your telemetry with lstk's" section, covering the `machineId`/`machine_id` spelling difference and that it goes absent together with `sessionId`
- [x] 4.4 `docs/extensions-authoring.md`: extend the global-flags conveyance note with `endpointUrl` and repeat the flag-position caveat — only `lstk --endpoint-url … myext` reaches lstk's parser, while `lstk myext --endpoint-url …` is forwarded verbatim, exactly like `--json`
- [x] 4.5 Update the CLAUDE.md Extensions section's inline context-field list
