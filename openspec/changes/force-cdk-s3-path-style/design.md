## Context

`lstk cdk` points CDK at LocalStack purely through environment variables. The original cdk-proxy design (`openspec/changes/archive/2026-06-16-add-cdk-proxy-command/design.md`) concluded that the endpoint was reachable this way but **S3 addressing style was not**: `forcePathStyle` was a code-level client argument only, so `endpoint.S3Addressing`'s `pathStyle` return value was documented as "informational only" and discarded at `internal/iac/cdk/cli/exec.go`. That conclusion is now out of date — current CDK reads `CDK_S3_FORCE_PATH_STYLE`.

CDK's own fallback, from `@aws-cdk/private-tools/lib/s3-path-style` (bundled into `aws-cdk/lib/index.js`, consumed by `cdk-assets-lib`'s `DefaultAwsClient.s3Client()` and `toolkit-lib`'s `SDK.s3()`):

```js
function forceS3PathStyle() {
  if (process.env.CDK_S3_FORCE_PATH_STYLE) return true;
  const endpoint = process.env.AWS_ENDPOINT_URL_S3 ?? process.env.AWS_ENDPOINT_URL;
  if (endpoint && isLoopbackEndpoint(endpoint)) return true;
  return undefined;   // → SDK default → virtual-host
}
// isLoopbackEndpoint: hostname === "localhost" || startsWith("127.") || "::1"
```

That test is a literal string prefix on the hostname, not an address check. Three consequences, all confirmed against LocalStack 2026.8.1 (pro) with aws-cdk 2.1140.0:

1. `localhost` and `127.0.0.1` get path style, so the common local case works by luck of the special case.
2. `*.localstack.cloud` hosts get virtual-host addressing, which works because lstk derives an `s3.`-prefixed endpoint and LocalStack recognizes `<bucket>.s3.localhost.localstack.cloud`.
3. **Every other hostname breaks.** LocalStack does not recognize the resulting `<bucket>.<host>` as a virtual-host bucket, so it parses the request as path-style: `PUT /<key>` is read as `CreateBucket(<key>)`. Verified directly — a virtual-host `PUT` to an unrecognized host created a *bucket* named after the key while the target bucket stayed empty. With a real asset body the request dies parsing the zip as XML: `Unable to parse request (not well-formed (invalid token): line 1, column 2), invalid XML received: b'PK\x03\x04...'`.

Case 3 covers the realistic deployment shapes: `http://localstack:4566` (docker compose service name), a Kubernetes service DNS name, or any LocalStack on another host.

## Goals / Non-Goals

**Goals:**

- CDK asset publishing works for every endpoint lstk resolves, not just loopback literals and `*.localstack.cloud`.
- One mechanism, no host-dependent branching — the failure mode this fixes came from a host-shape heuristic, and adding a second one invites the same class of bug.
- Preserve the caller's ability to direct S3 themselves.

**Non-Goals:**

- Changing `endpoint.S3Addressing` itself. Its `pathStyle`/`s3.`-prefix logic stays exactly as it is for `lstk terraform`, which consumes both return values in its generated override. Only the CDK proxy stops calling it.
- Raising the 2.177.0 minimum CDK version. The behavior is version-gated instead, so CDKs below the flag's introduction keep working exactly as they do today.
- Touching `lstk terraform`, which sets `s3_use_path_style` in its generated override and does not have this problem.

## Decisions

### Addressing mode and host prefix are one setting, gated on the CDK version

The two halves of this change are not independent knobs — each addressing mode requires its own host shape, and mixing them fails:

```
   CDK >= 2.1138.0                    CDK < 2.1138.0
   ──────────────                     ──────────────
   flag honoured                      flag ignored
   → path style                       → virtual-host (SDK default)
   → needs the BARE host              → needs the s3.-PREFIXED host
     (LocalStack routes by path;        (LocalStack only recognizes a
      sandbox cert covers it)            virtual-host bucket when the
                                         `s3.` label is present)
```

Measured against LocalStack 2026.8.1, the same bucket, path-style vs virtual-host by host shape:

| request | result |
|---|---|
| `PUT <bucket>.s3.localhost.localstack.cloud/key` | 200, object stored in the bucket |
| `PUT <bucket>.localhost.localstack.cloud/key` | 200, but **a bucket named `key` was created** — the virtual host was not recognized, so it parsed as path-style `PUT /key` = CreateBucket |

And end to end with a real old CLI (aws-cdk 2.1137.0, the last release before the flag), same project, same emulator:

| S3 endpoint given to old CDK | result |
|---|---|
| `localhost.localstack.cloud:4566` (bare) | `exception while calling s3 with unknown operation … invalid XML` |
| `s3.localhost.localstack.cloud:4566` (prefixed) | both assets published, deploy succeeds |

So lstk picks the **pair** that matches the CDK in front of it: on 2.1138.0+ it sets the flag and passes the base endpoint through unprefixed; below that it sets no flag and keeps the `s3.` prefix from `endpoint.S3Addressing`, which is exactly today's behavior.

This costs no extra work at runtime: `CheckVersion` already shells out to `cdk --version` and parses `major.minor.patch` on every invocation for the 2.177.0 floor check, then discards the result. The gate is that value, returned instead of thrown away.

**Alternative considered — raise the floor to 2.1138.0** and keep one unbranched path. Rejected: 2.1138.0 shipped 2026-08-19, while the current floor 2.177.0 spans roughly nineteen months of releases below it. Hard-failing that whole range to fix a bug most of them do not have is a far worse trade than a version branch.

**Alternative considered — drop the prefix unconditionally** (what this change did before the gate). Rejected once measured: it regresses every CDK in `[2.177.0, 2.1137.x]` on the **default local path**, which has always worked. That is a much larger blast radius than the sandbox bug being fixed.

**Known unsupportable combination.** Old CDK against a LocalStack Cloud sandbox has no working configuration at all: virtual-host addressing is forced by the CLI, and neither `s3.<id>.sandbox…` nor `<bucket>.<id>.sandbox…` is covered by the single-label wildcard certificate. Sandbox support requires CDK 2.1138.0+. lstk does not attempt to detect and pre-empt this — that would mean inferring "is a sandbox" from the hostname, the same host-shape heuristic this change exists to remove.

### Set the variable unconditionally rather than from `S3Addressing`'s `pathStyle`

(Within the 2.1138.0+ branch.)

`endpoint.S3Addressing` already returns exactly the hosts that need path style, so deriving the variable from it is tempting. Rejected in favour of setting it always:

- Path style works against LocalStack on *every* host lstk resolves. Verified A/B on a virtual-host-capable endpoint (`localhost.localstack.cloud`, so the derived S3 endpoint is `s3.localhost.localstack.cloud`), captured through a logging proxy:

  | | actual request | result |
  |---|---|---|
  | without the variable | `PUT host=cdk-hnb659fds-assets-…s3.localhost.localstack.cloud path=/<hash>.zip` | exit 0 |
  | with the variable | `PUT host=s3.localhost.localstack.cloud path=/cdk-hnb659fds-assets-…/<hash>.zip` | exit 0 |

  `cdk bootstrap` and `cdk gc --action=print` — which reach S3 through `toolkit-lib`'s `SDK.s3()` rather than `cdk-assets` — also both succeed with it set.

- The conditional version has a live footgun: `pathStyle` is computed from the *base* endpoint, while `AWS_ENDPOINT_URL_S3` may be replaced afterwards by the caller's override. A caller overriding to `http://localstack:4566` from a base of `localhost.localstack.cloud` would compute `pathStyle=false` from the wrong URL and stay broken. Getting this right means recomputing from the final endpoint — more moving parts for no behavioural gain.

- Unconditional keeps `S3Addressing`'s `pathStyle` out of the CDK path entirely, which matches its remaining honest use (terraform's `s3_use_path_style`).

**Alternative considered — set `forcePathStyle` some other way.** There is none. The variable is the only env-level lever; the addressing style is otherwise a code-level S3 client argument that an external subprocess cannot reach.

### Drop the `s3.` host prefix from the CDK proxy's `AWS_ENDPOINT_URL_S3` (on CDK 2.1138.0+)

Forcing path style is necessary but **not sufficient**. LocalStack Cloud sandbox and ephemeral instances serve a single-label wildcard certificate:

```
$ echo | openssl s_client -connect ls-<id>.sandbox.localstack.cloud:443 \
    -servername ls-<id>.sandbox.localstack.cloud | openssl x509 -noout -ext subjectAltName
X509v3 Subject Alternative Name:
    DNS:*.sandbox.localstack.cloud
```

`ls-<id>.sandbox.localstack.cloud` is covered. The `s3.ls-<id>.sandbox.localstack.cloud` that `S3Addressing` derives is one label deeper and is not, so the server aborts the handshake with `tlsv1 alert internal error` (alert 80). That failure precedes the first S3 request, which is why path style cannot rescue it — path style rewrites the request path, not the host. Measured against a live sandbox instance:

| addressing | host reached | result |
|---|---|---|
| path style, base host | `ls-<id>.sandbox.localstack.cloud` | **HTTP 200** |
| path style, `s3.` prefixed | `s3.ls-<id>.sandbox…` | TLS alert 80 |
| virtual-host, base host | `<bucket>.ls-<id>.sandbox…` | TLS alert 80 |

Only bare host + path style works, so on CDK 2.1138.0+ the proxy sets `AWS_ENDPOINT_URL_S3` to the resolved base endpoint verbatim. The prefix and the addressing mode are one decision, not two: the prefix exists only to serve virtual-host addressing, which that branch no longer uses. Below 2.1138.0 the prefix stays, because virtual-host addressing is unavoidable there and needs it.

**Alternative considered — keep the prefix and special-case sandbox hosts.** Rejected: it reintroduces the host-shape heuristic this change exists to remove, and certificate depth is not something lstk can infer from a hostname.

**Alternative considered — leave the prefix and document the two-variable workaround** (`AWS_ENDPOINT_URL_S3=<bare host>` plus `CDK_S3_FORCE_PATH_STYLE=1`). Rejected, and note it is not even available: under the ownership rule below, lstk strips the caller's `CDK_S3_FORCE_PATH_STYLE`, so that combination resolves to virtual-host and fails. Without this decision, sandbox users would have no working configuration at all.

### Suppress the default when the caller sets `AWS_ENDPOINT_URL_S3`

`AWS_ENDPOINT_URL_S3` is a documented `lstk cdk` override (`cmd/cdk.go` help text). A caller who sets it is steering S3 deliberately, so lstk leaves the addressing mode to CDK's own default there rather than overriding a decision the caller may have made on purpose. This is the one case where virtual-host addressing remains reachable through lstk.

The suppression signal is the caller's environment, not the *effective* S3 endpoint: `s3EndpointOverride()` already reads `AWS_ENDPOINT_URL_S3` for exactly this override, so the check is the value it returns being non-empty.

### `CDK_S3_FORCE_PATH_STYLE` is lstk's to set, and the caller cannot override it

lstk strips the variable from the inherited environment in **both** branches — the default one where it then sets `1`, and the suppressed one where it sets nothing. A caller-supplied value never reaches `cdk`.

Stripping only in the default branch would leave `AWS_ENDPOINT_URL_S3=… CDK_S3_FORCE_PATH_STYLE=1` as a back door to the state the caller is not meant to control, which would make "not overridable" untrue in exactly the case that matters. The suppression lever stays `AWS_ENDPOINT_URL_S3` and nothing else.

Honouring a caller value would also be a poor bargain given how CDK reads it: the check is `if (process.env.CDK_S3_FORCE_PATH_STYLE)`, so **any non-empty value is truthy** — `0` and `false` both force path style, and only the empty string is falsy. A caller typing `=0` to turn path style off would get it turned on. Owning the variable removes that trap rather than documenting around it; the truthiness detail stops being something a user can trip over, and survives only as a note for implementers not to write a falsy-looking value.

This makes the variable behave like every other entry in `BuildEnv`'s `managed` list, including `CDK_DISABLE_LEGACY_EXPORT_WARNING` — already a hard-coded, un-overridable, CDK-owned variable. The mechanism is the one that list already has: membership strips the caller's entry regardless of value, and an empty managed value means "strip but do not set", the same way an empty endpoint is handled. So the suppressed branch is an empty value, not a special case.

## Risks / Trade-offs

- **Older CDK releases ignore the variable** → Mitigated by the version gate, not by inertness. An earlier draft of this design claimed the variable is "inert on older CLIs, so this cannot regress anyone". That is false once the endpoint changes too: the flag is inert, but the unprefixed endpoint is not, and an old CLI then does virtual-host addressing against a host LocalStack does not recognize. Verified by regression against aws-cdk 2.1137.0. `CDK_S3_FORCE_PATH_STYLE` was introduced in 2.1138.0 (aws/aws-cdk-cli#1625; absent in 2.1137.0, present in 2.1138.0, confirmed by inspecting both published packages).

- **The version gate is a second code path** → It is exercised in only one direction by most developers, since a current CDK takes the new branch. Integration coverage must pin both, using fake CDKs that report a version on each side of 2.1138.0; that is cheap because the fake tool already answers `--version` from its config. The branch is deletable in one commit whenever the floor rises to 2.1138.0.

- **Sandbox/ephemeral instances were a second, independent bug** → Confirmed broken today and fixed here, but only because the `s3.` prefix is dropped as well as path style forced; either half alone still fails the TLS handshake. Verified end to end against a live sandbox instance. Any future change that reinstates a host-derived S3 endpoint on the CDK path must be re-tested there.

- **Real AWS is not a concern** → `BuildEnv` pins the endpoint at LocalStack and forces `test`/`test` credentials, so the CDK subprocess never reaches real S3 and AWS's path-style deprecation does not apply.

- **Loss of virtual-host coverage in normal use** → After this change, the `*.localstack.cloud` virtual-host path is only exercised when a caller sets `AWS_ENDPOINT_URL_S3`. That is a real reduction in what `lstk cdk` exercises by default; it is the intended trade, since the mode being dropped is the one that fails on a whole class of hosts.

## Migration Plan

Additive and self-applying — no user action, no config change, no flag. The first `lstk cdk` run after upgrading picks it up: users on CDK 2.1138.0+ get path style and the unprefixed endpoint, users below it keep exactly what they have today, and they cross over whenever they upgrade CDK. Rollback is reverting to the old branch for everyone.

Three pieces of now-wrong guidance should be retired with the code change, or they will keep sending readers the wrong way:

- the "CDK has no env-only lever for path style, so a `pathStyle==true` result there is informational only" note on `endpoint.S3Addressing` (`internal/endpoint/s3.go`);
- the loopback S3 warning in `cmd/cdk.go`, which fires on the `127.0.0.1` case that has always worked;
- the archived cdk-proxy design's "S3 path style is unreachable" framing, which this change supersedes.

## Open Questions

- **Resolved**: `CDK_S3_FORCE_PATH_STYLE` was introduced in aws-cdk **2.1138.0** (aws/aws-cdk-cli#1625, landed 2026-08-18). Absent in 2.1137.0, present in 2.1138.0, verified by inspecting both published packages. This is the gate threshold, so it is load-bearing rather than informational.
- Should the version gate be removed by raising the floor to 2.1138.0 at some later point, and if so on what signal — a support policy, or telemetry on the CDK versions users actually run?
- Should `lstk cdk` warn when it detects old CDK against an endpoint whose S3 host will not work (the sandbox case), instead of letting CDK surface `unknown operation … invalid XML`? Left open because the detection needs a hostname heuristic this design otherwise avoids.
