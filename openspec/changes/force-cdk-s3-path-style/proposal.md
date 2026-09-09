## Why

`lstk cdk deploy` fails to publish assets in two distinct situations: whenever the resolved endpoint host is an ordinary hostname — `http://localstack:4566` (docker compose), a Kubernetes service DNS name, or any remote host — and against every LocalStack Cloud sandbox/ephemeral instance, where the `s3.`-prefixed endpoint lstk derives has no TLS certificate. CDK's asset publisher only auto-selects S3 path-style addressing for loopback literals (`localhost`, `127.*`, `::1`); for every other host it uses virtual-host addressing, which LocalStack does not recognize unless the host matches one of its own patterns. The upload is then silently reinterpreted as a bucket-level request and the deploy dies with `exception while calling s3 with unknown operation: Unable to parse request ... invalid XML received: b'PK\x03\x04...'` — the Lambda zip being fed to an XML parser.

lstk already knows path style is required (`endpoint.S3Addressing` returns it) but discards that decision, and CDK now exposes `CDK_S3_FORCE_PATH_STYLE` as the env-only lever the original cdk-proxy design concluded did not exist.

## What Changes

- `lstk cdk` sets `CDK_S3_FORCE_PATH_STYLE=1` in the `cdk` subprocess environment by default, so CDK uses path-style S3 addressing for every endpoint lstk resolves.
- The variable is set unconditionally regardless of what `endpoint.S3Addressing` reports about the host — path style works against LocalStack for virtual-host-capable and non-virtual-host-capable hosts alike, so the derived `pathStyle` value stays unused for this purpose.
- **Exception**: when the caller has explicitly set `AWS_ENDPOINT_URL_S3`, lstk does not set `CDK_S3_FORCE_PATH_STYLE`. That override already means "I am directing S3 myself", so lstk leaves the addressing decision to CDK's own default.
- `CDK_S3_FORCE_PATH_STYLE` is lstk's to set, not the caller's: it is stripped from the inherited environment in both branches above, so a caller-supplied value never reaches `cdk`. Setting it alongside `AWS_ENDPOINT_URL_S3` is not a back door to forcing path style on the override path.
- `lstk cdk` stops applying the `s3.` host prefix when deriving `AWS_ENDPOINT_URL_S3`, passing the resolved base endpoint through verbatim. The prefix exists solely to make virtual-host addressing work; with path style forced it is dead weight, and on LocalStack Cloud sandbox/ephemeral instances it is fatal — their certificate is a single-label wildcard (`*.sandbox.localstack.cloud`), so `s3.<id>.sandbox.localstack.cloud` has no certificate and the TLS handshake fails with `tlsv1 alert internal error` before any S3 request is sent. Forcing path style cannot rescue this: it changes the request path, not the host.
- `endpoint.S3Addressing` itself is unchanged. It keeps serving `lstk terraform`, which consumes both of its return values, and the CDK proxy still calls it on the pre-2.1138.0 branch to derive the prefixed endpoint.

All of the above applies on **aws-cdk 2.1138.0 and newer**, the release that introduced `CDK_S3_FORCE_PATH_STYLE`. Below it the flag is ignored, CDK falls back to virtual-host addressing, and virtual-host addressing needs the `s3.`-prefixed host — so lstk keeps today's behavior there: no flag, prefix retained. The two settings are one matched pair, and applying half of it to an old CLI regresses the default local path (verified against aws-cdk 2.1137.0). lstk already parses `cdk --version` on every invocation for its floor check, so the gate is free.

The 2.177.0 minimum version floor is unchanged; users cross to the new behavior when they upgrade CDK, and the gate can be deleted if the floor ever rises to 2.1138.0.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cdk-proxy`: the subprocess environment requirement gains S3 addressing — `CDK_S3_FORCE_PATH_STYLE=1` is set by default, and suppressed when the caller sets `AWS_ENDPOINT_URL_S3`.

## Impact

- `internal/iac/cdk/cli/exec.go` — `BuildEnv` gains the variable; `Run` branches on the CDK version to choose the endpoint/addressing pair.
- `internal/iac/cdk/cli/version.go` — `CheckVersion` returns the parsed version it already computes, instead of discarding it, so `Run` can gate on it without a second `cdk --version` exec.
- `internal/iac/cdk/cli/defaults.go` — a constant for the 2.1138.0 gate threshold, beside the existing floor constants.
- `internal/iac/cdk/cli/env.go` — `s3EndpointOverride()` already reports the override; it becomes the suppression signal too.
- `internal/iac/cdk/cli/exec_test.go` — unit coverage for set / suppressed / caller-supplied cases.
- `test/integration/` — an end-to-end test asserting the variable reaches the `cdk` subprocess, using the existing fake-tool harness.
- `cmd/cdk.go` — help text lists the new variable alongside the other supported ones.
- Stale guidance to retire: the "informational only" note on `endpoint.S3Addressing`, the loopback S3 warning in `cmd/cdk.go`, and the cdk-proxy design's "path style is unreachable" claim.
- No change to `lstk terraform`, which sets `s3_use_path_style` in its generated override and is unaffected.
