# Writing an lstk extension

lstk supports Git-style extensions. When you run `lstk <name>` and `<name>` is not a built-in command, lstk looks for an executable called `lstk-<name>` and runs it, forwarding your arguments and the child's input/output and exit code. Anyone can add a command to lstk by putting an `lstk-<name>` executable on their `PATH` — in any language, open or closed source. There is **no manifest** and no registration step.

## The contract at a glance

- **Name it `lstk-<name>`** and put it on `PATH`. `lstk <name> ...` will run it; `lstk help` will list it.
- **You are invoked as `lstk-<name>`.** Your `argv[0]` is the base name `lstk-<name>` (plus `.exe` when your file carries that suffix on Windows), never a full path, and it is the same whether lstk found you on `PATH`, next to its own binary, or inside LocalStack's bundle. Strip a trailing `.exe` before comparing. A `<name>` starts with a letter or digit and uses only letters, digits, hyphens and underscores.
- **Your arguments are forwarded verbatim.** Everything after `<name>` is yours — lstk does not parse it. Define and parse your own flags however you like, including flags that happen to share a name with an lstk global flag.
- **lstk's global flags are consumed before the name.** `lstk --non-interactive <name> --foo` runs your extension with just `--foo`; the resolved global state reaches you via environment variables (below), not on your command line.
- **Exit code and streams pass through.** Your exit status becomes lstk's exit status, and your stdin/stdout/stderr are wired straight to the terminal.

## Runtime context: `LSTK_EXT_API_VERSION` and `LSTK_EXT_CONTEXT`

lstk passes everything you need through two environment variables, so you never have to read lstk's config files or re-implement emulator discovery. Your existing environment (PATH, locale, proxy settings, …) is inherited unchanged.

- **`LSTK_EXT_API_VERSION`** — an integer version of this contract, kept as a plain value so you can check it *before* parsing anything.
- **`LSTK_EXT_CONTEXT`** — a single JSON object with the resolved runtime context:

```json
{
  "configDir": "/home/you/.config/lstk",
  "authToken": "ls-...",
  "nonInteractive": true,
  "json": false,
  "sessionId": "6f1c9a2e-7b4d-4c3a-9f10-2d8e5b7a1c43",
  "machineId": "dkr_0a1b2c3d4e5f",
  "endpointUrl": "http://localhost:4566",
  "emulators": [
    { "type": "aws", "endpoint": "http://localhost.localstack.cloud:4566", "port": "4566" }
  ]
}
```

| Field | Type | Notes |
| --- | --- | --- |
| `configDir` | string | lstk's resolved config directory. Always present. |
| `authToken` | string | The user's resolved LocalStack auth token. **Omitted** when not authenticated. |
| `nonInteractive` | bool | `true` when the user passed `--non-interactive` or stdout is not a TTY. When true, do not prompt. |
| `json` | bool | `true` when the user passed `--json`. Setting `--json` also forces `nonInteractive` to `true`. lstk makes no decision about your output format — decide for yourself whether to honor it. |
| `sessionId` | string | lstk's telemetry session id for this invocation. **Omitted** when lstk's telemetry is disabled. See [Correlating your telemetry with lstk's](#correlating-your-telemetry-with-lstks). |
| `machineId` | string | lstk's anonymized machine id — an irreversible hash, not your Docker or system id. **Omitted** when lstk's telemetry is disabled. See [Correlating your telemetry with lstk's](#correlating-your-telemetry-with-lstks). |
| `endpointUrl` | string | An externally-managed emulator lstk was pointed at, from `--endpoint-url`, `LSTK_ENDPOINT_URL`, or `AWS_ENDPOINT_URL`. **Omitted** when none was set. Conveyed **verbatim and unvalidated** — see [Targeting an external emulator](#targeting-an-external-emulator). |
| `emulators` | array | One entry per running LocalStack emulator: `{ "type", "endpoint", "port" }`. An **empty array** `[]` when none are running. |

`emulators` can hold **more than one** entry — lstk may run an AWS, a Snowflake, and an Azure emulator at the same time. Don't assume a single endpoint: select the one(s) your extension needs by `type`, and handle the empty case. `authToken` is **omitted, not set empty**, when the user is not authenticated — check for its presence.

### Reading the context

Use any JSON parser. For example, with `jq` in a shell extension:

```sh
aws_endpoint=$(printf '%s' "$LSTK_EXT_CONTEXT" | jq -r '.emulators[] | select(.type=="aws") | .endpoint')
token=$(printf '%s' "$LSTK_EXT_CONTEXT" | jq -r '.authToken // empty')
```

### Versioning and feature detection

Two different questions, two different mechanisms — don't use the version to answer the first:

**"Is the field I need present?" → check the JSON, not the version.** lstk adds new fields to `LSTK_EXT_CONTEXT` *without* changing `LSTK_EXT_API_VERSION`, so the version cannot tell you a newer field exists. An older lstk simply omits a field it doesn't have, so test for it directly and degrade or fail with a clear message:

```sh
region=$(printf '%s' "$LSTK_EXT_CONTEXT" | jq -r '.region // empty')
if [ -z "$region" ]; then
  echo "this command needs a newer lstk (no 'region' in the extension context)" >&2
  exit 1
fi
```

Any field added after the first release is **distinguishable when absent** (a missing key / null), precisely so this check works.

**"Did a breaking change happen?" → that's all the version tracks.** `LSTK_EXT_API_VERSION` is bumped *only* when a field is removed or repurposed — never for additions. If you were built against version 1, optionally refuse a higher version you don't understand (this guards the one case presence-checks can't catch: a field whose *meaning* changed):

```sh
if [ "${LSTK_EXT_API_VERSION:-0}" -gt 1 ]; then
  echo "this extension was built for an older lstk extension contract" >&2
  exit 1
fi
```

lstk performs **no** compatibility check for you — it runs any resolvable `lstk-<name>`. Confirming the fields you use (by presence) and the contract generation (by version) is your responsibility.

### Conveyance of global flags

Each lstk global flag that affects behavior is conveyed as a field of `LSTK_EXT_CONTEXT` rather than being forwarded on your command line (today: `nonInteractive`, `json`, `endpointUrl`). This is what lets you own your entire flag namespace without colliding with lstk. As lstk adds global flags, they appear as additional fields — additively, under the same `LSTK_EXT_API_VERSION` major version.

Position matters for all of them, in the same way: only `lstk --endpoint-url http://host:4566 myext` reaches lstk's parser and becomes `endpointUrl`. Written after your name, `lstk myext --endpoint-url http://host:4566` is forwarded to your argv verbatim and lstk never sees it — exactly like `--json`. The environment variables work in either spelling, since they're not positional at all.

### Targeting an external emulator

`endpointUrl` is present when the user pointed lstk at an emulator it did not start — docker compose, host networking, CI, another machine, or a cloud-hosted ephemeral instance. lstk resolves it from `--endpoint-url` first, then `LSTK_ENDPOINT_URL`, then `AWS_ENDPOINT_URL`, and conveys the winner:

```sh
endpoint_url=$(printf '%s' "$LSTK_EXT_CONTEXT" | jq -r '.endpointUrl // empty')
```

**The value is unvalidated.** lstk applies that precedence and nothing else: it does not check that the value parses as a URL, does not normalize it, and does not check that anything is listening. Whatever the user set arrives byte for byte, so parse it yourself and handle a malformed or unreachable value rather than assuming it is usable. This is deliberate — an extension that diagnoses a broken endpoint has to be able to see the actual mistake, and would be useless if lstk refused to dispatch it.

`endpointUrl` and `emulators` answer different questions and neither replaces the other: `emulators` is what lstk found running under local Docker, while `endpointUrl` is what it was *told* to target. A user with both set has a local emulator running and is aiming somewhere else; if your extension acts on an endpoint, prefer `endpointUrl` when it is present. Absence means either that no endpoint source was set — the default local target — or that lstk predates the field; in both cases fall back to your default endpoint.

### Correlating your telemetry with lstk's

lstk records every extension invocation as its own analytics event (`ext:<name>`, carrying the duration and exit code). If your extension reports analytics too, `sessionId` is what joins the two: it is lstk's session id for the invocation that dispatched you, so stamping it on your own events makes the join exact instead of guessing from a machine id and a timestamp. `machineId` is the companion for the other axis: it is the anonymized machine identity lstk stamps on its own events, so reusing it makes your events and lstk's report as one machine instead of two. Note the spelling difference — the context fields are `sessionId` and `machineId`, while lstk's analytics wire format calls the same values `session_id` and `machine_id`.

```sh
session_id=$(printf '%s' "$LSTK_EXT_CONTEXT" | jq -r '.sessionId // empty')
machine_id=$(printf '%s' "$LSTK_EXT_CONTEXT" | jq -r '.machineId // empty')
```

Reuse `machineId` rather than deriving your own: it is already the final hashed value (never a raw Docker or system id, so there is nothing further to anonymize), and deriving it independently costs a Docker round-trip that can fail and can disagree with lstk's answer.

**Absence is ambiguous, by design.** Both fields are omitted when lstk's telemetry is disabled — a disabled lstk computes neither, so they always appear and disappear together — and both are also absent on an lstk released before they existed. You cannot tell those two cases apart, so don't try. Treat absence as "no correlation available" and carry on: generate or derive your own ids if you need them, and never make either field a hard requirement.

## How LocalStack's bundled extensions differ

Everything above applies to bundled extensions too, with one mechanical
difference: they are not separate `lstk-<name>` files. LocalStack ships one
multi-call binary, `bundled-extensions`, next to `lstk`, and lstk executes it
with `argv[0]` set to `lstk-<name>` for whichever command was requested. The
list of commands it provides is the descriptions file `lstk-extensions.toml`
beside it; a name that file does not list is never handed to the bundle. Read
`os.Args[0]` (or your language's equivalent) to find out which extension you are
being asked to be. The value is exactly `lstk-<name>`, with no path and no
`.exe`, and lstk only ever hands the bundle a name the toml lists, so a lookup
miss inside the binary means the toml and the binary disagree: report it
loudly rather than guessing.

The binary is the only copy on disk — there is no `lstk-<name>` file per
command — so it is reachable only as `lstk <name>`. It nonetheless sits in the
install directory next to `lstk`, where somebody will eventually find it and
run it directly. Handle that: when `LSTK_EXT_API_VERSION` is unset there is no
runtime context to work from, so print a short message saying the binary is
part of lstk and naming the command to use instead, then exit non-zero. Do not
fall back to defaults. See [extensions-bundling.md](extensions-bundling.md)
for how the bundle is built and shipped.

## Help descriptions

`lstk --help` lists installed extensions by command name. One-line descriptions are shown **only for extensions LocalStack bundles with lstk**, from a static descriptions file LocalStack ships with them. Third-party and `PATH`-installed extensions are listed by name only (the same as Git's `git help -a`). lstk never executes an extension to render help, so listing is always side-effect-free.

## Authorizing the user (and why it cannot rely on lstk)

If your extension is free to use, you do not need to authorize anything — just do your work.

If your extension must be restricted (for example a paid feature), **authorization is entirely your responsibility.** lstk hands you the user's auth token in `LSTK_EXT_CONTEXT.authToken` and dispatches; it makes no entitlement decision. Authorize by making a server-side check with that token — typically a call to the LocalStack platform that returns whether this user is entitled — and refuse to perform the protected work when it does not pass:

```sh
token=$(printf '%s' "$LSTK_EXT_CONTEXT" | jq -r '.authToken // empty')
if [ -z "$token" ]; then
  echo "this command requires a LocalStack account; run 'lstk login'" >&2
  exit 1
fi
# Verify entitlement server-side using the token, then proceed or refuse.
```

**Security note.** lstk is open source and can be rebuilt with any check removed or any value forged. So no protection can depend on lstk behaving honestly — a check lstk performs is a UX speed bump, not a control. The only durable boundary is something a modified lstk cannot produce: a server-side decision keyed to the user's token, or a signature you verify yourself. Anchor real enforcement there. An extension whose value is purely local logic cannot be protected on the client at all (the standard DRM reality); gate the valuable behavior server-side.
