# Configuration Parsing Reference (internal/config)

Detail moved out of the root CLAUDE.md; see the root file for the config search order and creation policy.

## Container name

Each `[[containers]]` block may set an optional `container_name` to override the derived container name. The key is `container_name` rather than a bare `name` because a `[[containers]]` block already carries several names (the image, the emulator type) and an unqualified `name` reads ambiguously against them. The struct field is `CustomName` (not `Name`) because `ContainerConfig.Name()` is already a method — the same pairing as `CustomImage`/`Image()`. `Name()` returns `CustomName` when set, otherwise `defaultName()` (`localstack-{type}`, or `localstack-{type}-{tag}` when the tag is set and not `latest`).

The name is load-bearing, not cosmetic: `start.go` exports it as `MAIN_CONTAINER_NAME`, which the emulator uses to introspect itself over the Docker socket and to derive the names of containers it spawns (e.g. `<main-container-name>-lambda-<fn>-<id>`). Setting `MAIN_CONTAINER_NAME` in an `[env.*]` profile instead is a trap — it never reached `docker run --name`, so the container's real and self-reported names disagreed; `startOnce` now warns and points at this field.

`VolumeDir()`'s default deliberately uses `defaultName()`, **not** `Name()`, so setting or changing `container_name` never silently orphans the persistence directory (and the path stays byte-identical to what existing users already have). Per-instance state is expressible explicitly via `volume`/`volumes`.

Validation is `validate.ContainerName` (`internal/validate`), called from `ContainerConfig.Validate()`; it mirrors Docker's own `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$` rule so an invalid name fails at config load rather than at container creation. A custom `container_name` survives a `lstk start --type` switch silently (like `port`/`env`/`snapshot`) — it identifies the user's container, it does not pin a product the way `image` does.

## Container image override

Each `[[containers]]` block may set an optional `image` to override the default Docker Hub image (e.g. an internal registry mirror or a locally loaded offline image). `ContainerConfig.Image()` returns `image` as-is when it already carries a tag (so the separately-configured `tag` is dropped in that case), otherwise it appends `tag` (or `latest`); the default `localstack/<product>:<tag>` is used when `image` is unset.

## Exposing additional ports (`expose_ports`)

Each `[[containers]]` block may set `expose_ports` to publish container ports beyond the ones lstk publishes on its own (the edge port, the extra `GATEWAY_LISTEN` ports, the 4510-4559 service range). The motivating case (DEVX-994) is the emulator's DNS server: `expose_ports = [53]` replaces the v1 CLI's `--host-dns` flag. There is deliberately no CLI flag — the ticket asked for a config setting, and every consumer of the value is the start path.

Grammar per entry, parsed by `parseExposePort`/`ExposedPorts` (`internal/config/containers.go`):

- Values may be TOML **integers or strings** in the same list (`expose_ports = [53, "5354:5353/udp"]`). Ints work because viper's decoder sets `WeaklyTypedInput`, so the `[]string` field accepts numbers; the mixed form is pinned by `TestGet_ExposePortsAcceptsIntsAndStrings`.
- A string is `[host:]container[/proto]`; a bare port publishes host-port == container-port. Ports are canonicalized through `strconv` (`"0053"` → `"53"`), so two spellings of the same port collide as expected.
- **An entry that names no protocol expands to both tcp and udp.** DNS serves queries over both, so `[53]` alone has to be enough; publishing a protocol nothing listens on costs nothing. An explicit `/tcp` or `/udp` publishes only that protocol.
- Two entries that would make Docker key the same binding twice — same container port + protocol with different host ports, or the same host port + protocol for different container ports — are a **validation error** (`Validate()` calls `ExposedPorts()`), because Docker would silently keep only one. Exact duplicates are de-duplicated instead.

Consumption is `mergeExposePorts` (`internal/container/expose.go`), appending `runtime.PortMapping`s with `Optional: false` — an explicitly requested port is a demand, so a busy or unbindable host port fails the start (same rule as a user-supplied `GATEWAY_LISTEN`). Entries clashing with a port lstk already publishes are skipped: silently when they ask for exactly what lstk already does, with a warning when the automatic mapping wins over what the user wrote. See `internal/container/CLAUDE.md` for the preflight's UDP exclusion.

## Volume Mounts

Each `[[containers]]` block accepts a `volumes` list of Docker-style `"host:container[:ro]"` bind specs (e.g. for Snowflake init hooks mounted into `/etc/localstack/init/{boot,start,ready,shutdown}.d`). The persistence/cache mount to `/var/lib/localstack` is folded into this list: the entry whose container target is `/var/lib/localstack` (`persistenceTarget` in `internal/config/containers.go`) defines the host dir backing it, and that path is what `VolumeDir()`, `lstk volume path`, and `lstk volume clear` resolve. Resolution precedence in `VolumeDir()`: a `volumes` entry targeting `/var/lib/localstack` → the legacy singular `volume = "..."` field (still honored for backward compatibility) → the default OS cache dir. Setting the persistence dir via both `volume` and a `volumes` entry with differing sources is a validation error.

`volume` (singular) and `volumes` (plural) are not interchangeable in general — they overlap only for the persistence mount. `volume` *only* sets the persistence dir (always mounted to `/var/lib/localstack`); `volumes` is a superset that can set the persistence dir **and** arbitrary mounts. Two further distinctions: `volume` cannot express init hooks or any non-persistence mount, and the legacy `volume` value is used **verbatim** (no path resolution) whereas a `volumes` source is resolved. So `volume = "/data"` and `volumes = ["/data:/var/lib/localstack"]` are equivalent for persistence, but `volume = "./data"` is passed raw (and would become a Docker named volume) while `volumes = ["./data:/var/lib/localstack"]` resolves `./data` against the config dir.

Parsing/resolution lives in `parseVolume`/`ExtraVolumes` in `internal/config/containers.go`. The container target is validated with `path.IsAbs` (slash semantics) — never `filepath.IsAbs`, which rejects `/var/lib/localstack` on Windows. `splitVolumeSpec` rejoins a leading Windows drive letter (`C:\…`) onto the host source so its `:` is not mistaken for the host/container separator (Windows-guarded so a single-letter relative dir like `a:/data` stays valid elsewhere, matching Docker). Relative host sources resolve against the **config file's directory** and a leading `~/` is expanded — this is required because the Docker SDK treats a non-absolute source as a *named volume* rather than a bind mount. `start.go` mounts the persistence dir (creating it via `MkdirAll`) and appends `ExtraVolumes()`; extra sources are not created (`os.Stat` + error if missing) since init-hook entries are files, not dirs.
