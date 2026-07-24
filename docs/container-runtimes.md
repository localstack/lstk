# Container runtimes

`lstk` runs the LocalStack emulator as a container, so it needs a Docker-API-compatible runtime to talk to. It works with Docker Desktop, Rancher Desktop, Colima, OrbStack, Lima, and Podman — no extra flags needed in most cases.

## How lstk finds your runtime

`lstk` picks a daemon endpoint in this order:

1. **`DOCKER_HOST`**, if set. This always wins — `lstk` never overrides an explicit environment configuration.
2. **`DOCKER_CONTEXT`, or otherwise your current Docker CLI context**, if it isn't `default`. Rancher Desktop and OrbStack both register themselves as a context, so this self-maintains as you switch runtimes. `lstk` double-checks the context's socket is actually reachable before using it, so a stale context (e.g. pointing at a VM you deleted, or a `DOCKER_CONTEXT` naming one) is skipped rather than causing a hard failure.
3. **A list of known socket locations**, probed in this order: Docker Desktop, Rancher Desktop, Colima, OrbStack, Podman machine (macOS), Lima, then native Podman on Linux (rootful, then rootless).
4. **The Docker SDK's own default** (`/var/run/docker.sock`, or the default named pipe on Windows), if nothing above matched.

You can always force a specific endpoint by setting `DOCKER_HOST` yourself.

## Per-runtime notes

### Docker Desktop

The default. Nothing to configure.

### Rancher Desktop

Use the **dockerd (moby)** container engine — containerd mode isn't supported, since it doesn't expose a Docker-compatible socket. The socket is auto-detected at `~/.rd/docker.sock`. If detection doesn't find it, check that Rancher Desktop's "Administrative Access" setting is enabled, which also symlinks the socket to `/var/run/docker.sock`.

**Port 443**: the emulator publishes port 443 by default for HTTPS clients, and Rancher Desktop interferes with it in two ways: with Kubernetes enabled, its Traefik ingress holds the port; and without Administrative Access, Rancher Desktop cannot bind ports below 1024 at all. In both cases `lstk` starts without 443 and prints a warning — everything keeps working, but clients that hardwire HTTPS on port 443 (e.g. S3 virtual-host-style URLs without a port) must target `https://localhost:4566` instead; the edge port serves both HTTP and HTTPS. To get 443 back, free it with `rdctl set --kubernetes.options.traefik=false` (or disable Kubernetes in the preferences) and/or enable Administrative Access, then restart `lstk`.

### Colima / OrbStack / Lima

Auto-detected. Nothing to configure.

### Podman on Linux

Both rootful and rootless Podman are auto-detected:

- **Rootful**: `systemctl start podman` exposes the socket at `/run/podman/podman.sock`.
- **Rootless**: `systemctl --user start podman.socket` exposes the socket at `$XDG_RUNTIME_DIR/podman/podman.sock`.

Lambda functions started by the emulator may need extra Podman configuration under rootless Podman, since rootless bridge networking is restricted. See [LocalStack's Podman guide](https://docs.localstack.cloud/aws/capabilities/config/podman/) for the workaround.

### Podman machine (macOS)

Auto-detected. Podman's macOS "machine" backend runs Podman inside a VM and exposes a Docker-compatible socket, so Lambda functions work the same way they do under Docker Desktop or Colima.

## Troubleshooting

If `lstk` reports the runtime is not available, the error includes a suggested start command tailored to whatever runtime it detects on your machine (e.g. `rdctl start`, `colima start`, `podman machine start`, `systemctl --user start podman.socket`). Run that command and try again.

If detection picks the wrong runtime, or none at all, set `DOCKER_HOST` to point directly at the socket or endpoint you want `lstk` to use — it always takes priority over auto-detection.
