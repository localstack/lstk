# Container Start Reference (internal/container)

Detail moved out of the root CLAUDE.md.

## GATEWAY_LISTEN and host exposure

`GATEWAY_LISTEN` is not hardcoded — it is read from the container's resolved env (set it via an `[env.*]` profile referenced by the container's `env` field). When unset it defaults to `:4566,:443`. Parsing/derivation lives in `internal/container/gateway.go` (`parseGatewayListen`), mirroring the v1 CLI:

- The **container env value** blanks a `127.0.0.1` host so the gateway still listens on all interfaces inside the container (`:4566`), and preserves any non-loopback host verbatim.
- The **host publish IP** for *all* published ports (gateway ports + the 4510-4559 service range) is the host part of the first entry, defaulting to `127.0.0.1`. So `GATEWAY_LISTEN = "0.0.0.0:4566,0.0.0.0:443"` exposes the emulator beyond loopback (e.g. on an EC2/MicroVM host). This is threaded through as `runtime.ContainerConfig.BindHost` and applied in `internal/runtime/docker.go`.
- Gateway ports beyond the primary edge port (4566, which is published on the configured `port`) are published host-port == container-port, so listing an extra port like `:8443` publishes it. `servicePortRange()` covers only 4510-4559 now — 443 comes from the default `GATEWAY_LISTEN`.

## Offline / Enterprise degradation

There is no `--offline` flag. Instead `container.Start` degrades gracefully when internet requests fail (the common enterprise blockers: Docker Hub unreachable, proxy/TLS interception, license server unreachable):

- **Image pull**: if `rt.PullImage` fails but `rt.ImageExists` reports the image is already present locally, lstk warns and uses the local image instead of failing.
- **License pre-flight (image already local)**: when a pinned image is already present locally — so `pullImages` won't pull it — `tryPrePullLicenseValidation` skips the pre-flight check entirely (gated on `rt.ImageExists`), since the redundant network round-trip would otherwise block a fully-offline start; the container validates its own bundled license at startup. This is symmetric with the skip-pull behaviour above.
- **License pre-flight (server unreachable or erroring)**: when a check does run, `validateLicense` distinguishes a definitive server rejection (`isDefinitiveLicenseRejection`: HTTP 400/401/403 — fatal) from everything else: a transport-level failure (offline/proxy/cert) *and* any non-definitive status (5xx outage, 407 from a corporate proxy, ...) both skip the pre-flight check and let the container validate its own bundled license at startup.
- **License pre-flight (unsupported tag)**: when the server rejects the image tag *format* itself (`IsUnsupportedTag` — a 400 whose detail contains `licensing.license.format`, e.g. `dev` nightlies or custom enterprise-mirror tags), that is not a verdict on the license, so `validateLicense` skips the pre-flight with a warning and lets the container validate its own bundled license at startup — the same degradation as a transport failure. Genuine token/subscription rejections stay fatal. The invariant across all these paths: the pre-flight is a fail-fast optimization and must never block a start the container itself would accept.
- **Telemetry/update checks** are already best-effort and fail silently when offline.

`runtime.PullImage` always closes its `progress` channel (even when `ImagePull` fails early) so the local-image fallback path doesn't leak the progress goroutine. Pair this with a custom `image` in the config to point at a locally loaded image or an internal-registry mirror.

## License errors: cache invalidation and retry (DEVX-658)

A stale cached license (`license.json`) or a stale token (e.g. one that predates a license purchase) must never require a manual `lstk logout` to recover:

- **Definitive rejection (HTTP 400/401/403) in the pre-flight**: `validateLicense` deletes the cached `license.json` (the verdict invalidates it — a later start whose pre-flight is skipped must not keep mounting the stale copy). In interactive mode, `container.Start` then prompts to log in again (`auth.Relogin`: drops the stored token + cached license, reruns the browser login) and retries the start once with the fresh token. In non-interactive mode it emits an `ErrorEvent` pointing at `lstk logout && lstk login` / `LOCALSTACK_AUTH_TOKEN` and returns a silent error.
- **Startup license failure with a stale mounted cache**: when the container exits with license-related logs while a cached `license.json` that this run did *not* refresh was mounted (pre-flight skipped, e.g. image already local), `startContainers` returns a `licenseStartupError` instead of rendering the failure through `startupMonitor.handleFailure`, and `startWithLicenseRetry` drops the cache, re-validates against the license server (forced, bypassing the image-local skip), and retries the start once. No retry when the license was freshly fetched this run or no cache was mounted; a repeat failure is rendered by `handleFailure` as usual. The self-validating "not covered by your license" case keeps its dedicated messaging and is never retried.
- `StartOptions.AuthOptions` threads `auth.Option`s (e.g. `WithBrowserOpener`) into the internally constructed `auth.Auth` so tests of the re-login path never open a real browser tab.
