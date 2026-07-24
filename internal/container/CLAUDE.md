# Container Start Reference (internal/container)

Detail moved out of the root CLAUDE.md.

## Emulator discovery and external instances

Discovery is Docker-first with an HTTP fallback. `ResolveRunningContainerName` (running.go) is the Docker-only path: exact container-name match (`localstack-{type}`), then `FindRunningByImage` (known image repos + internal port). `ResolveEmulator` wraps it and, when Docker finds nothing — or Docker is unavailable (`rt == nil`) — probes `GET /_localstack/info` on the resolved host (`ProbeEmulatorInfo`, info.go: 2s timeout, requires 200 + JSON + non-empty `version` so an unrelated service can't false-positive). A successful probe yields an **external instance** (`ResolvedEmulator.External`): something lstk did not start, e.g. LocalStack running from source (`uv run -m localstack.runtime.main`) or reached via `LOCALSTACK_HOST`. The probe runs only on paths that previously errored, so container flows are unchanged and no latency is added to success paths.

Guard: `/_localstack/info` cannot identify the emulator product, so when Docker is healthy and a known LocalStack container of *any* type is running, `ResolveEmulator` treats the probe answer as that container and reports not-found — preserving the type-mismatch errors (e.g. `lstk terraform` with only Snowflake up). With Docker down the guard can't run; that looseness is accepted (from-source runs are overwhelmingly single-type).

Consumers: the proxies (`aws`, `az`, `terraform`/`cdk`/`sam`) go through `resolveReachableEmulator` in `cmd/emulator.go`; `reset` and `snapshot save/load` go through `FirstReachableEmulator` (running.go), which also demotes the Docker health check to lazy — "Docker is not available" is emitted only when the probe finds nothing either. `snapshot load`'s auto-starter requires Docker, so it runs only when Docker is healthy and nothing is reachable; an external instance is used as-is. `stop`, `logs`, `restart`, and `status` remain Docker-only (a non-container instance cannot be stopped or log-tailed by lstk; status/stop/logs messaging for external instances is a planned follow-up).

Integration tests for external instances live in `test/integration/external_instance_test.go`. **When adding a negative-path test** asserting "is not running"/"Docker is not available" on a probe-adopting command, pin `LOCALSTACK_HOST` to `deadLocalStackHost` (`127.0.0.1:1`) — otherwise the probe finds any real LocalStack on the developer's 4566 and the test flakes exactly on the machines this feature targets.

## GATEWAY_LISTEN and host exposure

`GATEWAY_LISTEN` is not hardcoded — it is read from the container's resolved env (set it via an `[env.*]` profile referenced by the container's `env` field). When unset it defaults to `:4566,:443`. Parsing/derivation lives in `internal/container/gateway.go` (`parseGatewayListen`), mirroring the v1 CLI:

- The **container env value** blanks a `127.0.0.1` host so the gateway still listens on all interfaces inside the container (`:4566`), and preserves any non-loopback host verbatim.
- The **host publish IP** for *all* published ports (gateway ports + the 4510-4559 service range) is the host part of the first entry, defaulting to `127.0.0.1`. So `GATEWAY_LISTEN = "0.0.0.0:4566,0.0.0.0:443"` exposes the emulator beyond loopback (e.g. on an EC2/MicroVM host). This is threaded through as `runtime.ContainerConfig.BindHost` and applied in `internal/runtime/docker.go`.
- Gateway ports beyond the primary edge port (4566, which is published on the configured `port`) are published host-port == container-port, so listing an extra port like `:8443` publishes it. `servicePortRange()` covers only 4510-4559 now — 443 comes from the default `GATEWAY_LISTEN`.

## Offline / Enterprise degradation

There is no `--offline` flag. Instead `container.Start` degrades gracefully when internet requests fail (the common enterprise blockers: Docker Hub unreachable, proxy/TLS interception, license server unreachable):

- **Image pull**: if `rt.PullImage` fails but `rt.ImageExists` reports the image is already present locally, lstk warns and uses the local image instead of failing.
- **License pre-flight (image already local)**: when a pinned image is already present locally — so `pullImages` won't pull it — `tryPrePullLicenseValidation` skips the pre-flight check entirely (gated on `rt.ImageExists`), since the redundant network round-trip would otherwise block a fully-offline start; the container validates its own bundled license at startup. This is symmetric with the skip-pull behaviour above.
- **License pre-flight (server unreachable)**: when a check does run, `validateLicense` distinguishes a definitive server rejection (`*api.LicenseError`, e.g. HTTP 403/400 — still fatal) from a transport-level failure (any other error — offline/proxy/cert). On a transport failure it skips the pre-flight check and lets the container validate its own bundled license at startup.
- **License pre-flight (unsupported tag)**: when the server rejects the image tag *format* itself (`IsUnsupportedTag` — a 400 whose detail contains `licensing.license.format`, e.g. `dev` nightlies or custom enterprise-mirror tags), that is not a verdict on the license, so `validateLicense` skips the pre-flight with a warning and lets the container validate its own bundled license at startup — the same degradation as a transport failure. Genuine token/subscription rejections stay fatal. The invariant across all these paths: the pre-flight is a fail-fast optimization and must never block a start the container itself would accept.
- **Telemetry/update checks** are already best-effort and fail silently when offline.

`runtime.PullImage` always closes its `progress` channel (even when `ImagePull` fails early) so the local-image fallback path doesn't leak the progress goroutine. Pair this with a custom `image` in the config to point at a locally loaded image or an internal-registry mirror.
