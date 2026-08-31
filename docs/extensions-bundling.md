# Bundled extensions: how they are built, shipped and found

This page is for whoever touches the release pipeline or debugs a bundled
extension in the field. It assumes no prior context. The author-facing
contract for writing an extension is in [extensions-authoring.md](extensions-authoring.md).

## What ships

LocalStack's own extensions (for example `lstk doctor`) are closed source and
ship **inside every lstk release** as two files placed next to the `lstk`
binary:

| File | What it is |
| --- | --- |
| `bundled-extensions` (`.exe` on Windows) | One multi-call binary providing every bundled extension. It decides which extension to be from the name it is invoked as (`argv[0]`), the way busybox and git do. |
| `lstk-extensions.toml` | A flat TOML table, `name = "one-line description"`, hand-written in the private extensions repository. For the bundle this file is load-bearing: it is the only record of which commands the binary provides. |

lstk never learns about bundled commands from directory contents. `lstk <name>`
reads the toml, sees `name` listed, and executes `bundled-extensions` with
`argv[0]` set to `lstk-<name>`, forwarding the arguments and the usual
`LSTK_EXT_API_VERSION` / `LSTK_EXT_CONTEXT` runtime context. A name the toml
does not list is not handed to the bundle; lstk falls through to standalone
`lstk-<name>` files and then `PATH`, exactly as for third-party extensions.
Every key in the toml must be a dispatchable command name (a letter or digit
first, then letters, digits, hyphens and underscores); lstk refuses to load a
bundle whose toml breaks that rule, and the release gate applies the same rule,
so a file that ships always loads.

Why one binary rather than a copy per name: copies cost roughly 30 MB per
extension in every archive and npm package, and symlink aliases do not survive
the tar/zip extractors or Windows. One binary has none of those problems. The
trade is that the toml must be present and correct whenever the binary is,
which the release gate below enforces and the runtime treats as a broken
install if violated.

## Where the files live on disk

lstk looks in one place: the directory of its own symlink-resolved executable
(`extension.BundledDir`). Each install method lands the two files there
without any layout work of its own.

| Channel | Directory | How it gets there |
| --- | --- | --- |
| Binary archive (`curl` + `tar`) | Wherever the user extracted the archive; the files sit at the archive root next to `lstk`. | GoReleaser `archives.files` entries in `.goreleaser.yaml`. |
| Homebrew | The cask's Caskroom staged directory, e.g. `/opt/homebrew/Caskroom/lstk/<version>/`. `bin/lstk` is a symlink into it; lstk resolves the link. | The cask stages the whole archive. Only `lstk` is symlinked into `bin`; the bundle is found via the directory, never via `PATH`. The post-install hook strips the macOS quarantine attribute from the **whole** staged directory so the bundle runs without a Gatekeeper prompt. |
| npm | The **platform** package, e.g. `node_modules/@localstack/lstk-darwin-arm64/`, not the `@localstack/lstk` wrapper. The launcher execs the Go binary from there, so that is where lstk's bundled dir resolves to. | `scripts/add-bundled-to-npm.sh` copies the files into each `dist/npm/lstk-<os>-<cpu>/` directory before `npm publish`, translating Node's platform names (`win32` → `windows`, `x64` → `amd64`), **and** adds them to that package's `files` allowlist. The publisher generates `"files": []`, which npm reads as "only `package.json` and the `bin` entry", so a plain copy would be silently dropped at publish. |

## The release pipeline

Everything happens in the `release` job of `.github/workflows/ci.yml`, in this
order. Every step failing fails the release.

1. **Select the bundle.** `bundled/extensions.version` (the only tracked file
   under `bundled/`) says which release of the private extensions repository to
   take. It says `latest` by default: the newest published bundle, with no
   routine bump to remember. Set it to an explicit tag (`v0.3.1`) to hold a
   build to one bundle.
2. **Fetch and verify.** `scripts/fetch-bundled-extensions.sh` resolves
   `latest` to a concrete tag **once**, prints it, downloads that tag's release
   assets with `gh release download`, and verifies every asset against the
   `checksums.txt` published in the same release. A missing manifest, an
   unlisted asset or a mismatching hash aborts. It then unpacks each platform
   archive and stages only two members: the binary as
   `bundled/<os>_<arch>/bundled-extensions[.exe]` (mode 0755) and the
   descriptions file as `bundled/lstk-extensions.toml` (taken once; every
   archive must carry an identical copy). It re-creates one `lstk-<name>`
   symlink to the binary per command, except on Windows (see "Running an
   extension directly" below), and records the names, sorted, in
   `bundled/bundle-commands.txt`: they are the bundle's own declaration of
   which commands the binary answers to. An archive with no aliases, or whose
   list differs from another platform's, aborts. It fails if any of lstk's six target platforms
   (`linux`/`darwin`/`windows` × `amd64`/`arm64`) has no archive. A platform can be exempted only by listing it in
   `UNSUPPORTED_PLATFORMS` at the top of the script, so a gap is always a
   visible choice.
3. **Gate the pairing.** `scripts/check-descriptions.sh bundled/linux_amd64`
   reads the command names from the toml (left-hand side only; values are
   never parsed) and compares them with the binary's own list in
   `bundle-commands.txt`. It fails if commands are described but there is no
   binary, if there is a binary but no commands are described, if the command
   list is missing or empty, or if the toml describes a command the bundle
   does not provide (lstk would exec the binary under a name it does not
   answer to). A command the bundle provides but the toml omits only warns:
   lstk will not expose it. Descriptions and the list are the same on every
   platform, so one directory is enough.
4. **Package.** GoReleaser adds the staged files to each archive at the root.
   The cask inherits them. `scripts/add-bundled-to-npm.sh` copies them into
   the platform packages and registers them in each package's `files`.
5. **Record.** After publishing, the job appends
   `Bundled extensions: <tag> (commit <sha>)` to the GitHub release notes.
   Job logs expire; release notes do not. This line is how you answer "which
   extensions build does this customer have?" from an lstk version number.

The credential is `LSTK_EXTENSIONS_READ_TOKEN`, a repository secret holding a
fine-grained personal access token with **read-only Contents** access to the
private extensions repository and nothing else. It is deliberately not
`PRO_ACCESS_TOKEN`: the release should hold no more access than it needs, and
a read-only token rotates independently.

### Re-running a published release

`latest` re-resolves on every invocation, so a naive re-run of an
already-published tag could ship different extension binaries under a version
already in the wild. The fetch step guards against this: before resolving, it
reads the existing GitHub release for the lstk tag being built and, if its
notes already carry a `Bundled extensions:` line, pins the fetch to that tag.
To do the same by hand, pass the recorded tag explicitly:

```bash
LSTK_EXTENSIONS_READ_TOKEN=... scripts/fetch-bundled-extensions.sh --tag v0.3.1
```

## What the private repository must publish

Each tagged release of `localstack/lstk-bundled-extensions` ships:

- one archive per lstk target platform, named
  `bundled-extensions_<tag>_<os>_<arch>.tar.gz` (`.zip` for Windows),
  containing at its root the multi-call binary `bundled-extensions` (`.exe` on
  Windows), `lstk-extensions.toml`, and one `lstk-<name>` alias entry (a
  symlink on Unix, a copy on Windows) for every command the binary answers
  to. They serve two purposes: they are the binary's own statement of its
  command list, which the release gate checks the toml against, and they are
  re-created in the packaged install so a command can be run directly (see
  below);
- the same `lstk-extensions.toml` in every archive, hand-authored, describing
  every command the binary provides — a described command with no
  implementation would show in `lstk --help` and fail when run;
- `checksums.txt` with a SHA-256 line for every archive.

The extensions team owns the descriptions text. lstk only validates that the
file and the binary agree.

## Local snapshot builds

Since the `bundled/` entries in `.goreleaser.yaml` are live, `goreleaser` fails
on an empty staging tree. Stage a bundle first, either for real:

```bash
LSTK_EXTENSIONS_READ_TOKEN=<token> scripts/fetch-bundled-extensions.sh
goreleaser release --snapshot --clean
```

or, without access to the private repository, with placeholders:

```bash
scripts/fetch-bundled-extensions.sh --stub
goreleaser release --snapshot --clean
```

`--stub` writes a tiny shell-script stand-in named `bundled-extensions` for
every platform, plus a toml describing one placeholder `doctor` command, and
prints a banner: **artifacts built from a stub bundle must never be
released.**

`scripts/check-bundled-packaging-sync.sh` runs on every PR next to
`goreleaser check`. It fails if `.goreleaser.yaml` references `bundled/` while
the release job has no fetch step, or the reverse. `goreleaser check` cannot
catch this itself because it only validates config syntax and never looks at
the filesystem. Both halves must land in the same PR.

The bash suites for all four scripts run with `make test-scripts`.

## Updates

**Homebrew and npm** replace the whole package directory on `lstk update`, so
the binary, the bundle and the toml are always replaced together, including
renames and removals.

**Binary channel** (`lstk update` downloading an archive itself): set-wise
replacement of `lstk` + `bundled-extensions` + `lstk-extensions.toml` is
pending (section 1 of the `add-bundled-extension-distribution` change). Until
it lands, the in-the-field updater replaces only `lstk`; the other two files
must be extracted from the archive by hand. Updating never deletes standalone
`lstk-<name>` files a user placed next to the binary.

## Running an extension directly

Where the channel allows it, a release also carries one `lstk-<name>` symlink
to `bundled-extensions` per command, so `lstk-doctor` can be run straight from
a shell. That is a convenience for trying an extension on its own; lstk never
resolves those links. It dispatches to the binary by `argv[0]` and takes its
command list from the toml, so a channel without them behaves identically.

| Channel | Aliases | Why |
| --- | --- | --- |
| Binary archive (tar.gz) | Yes | goreleaser preserves symlinks in tar.gz. |
| Homebrew cask | Yes | It stages the same archive. |
| Windows (zip) | No | Most Windows extractors turn a zip symlink into a small text file holding the target's name, which is worse than absent. |
| npm | No | `npm pack` silently drops symlinks from the published tarball. |

`lstk update` on the binary channel does not currently re-create them: its
extractor skips symlink entries. Whatever a fresh install put there is left
alone, so an updated install keeps the links it already had.

## Diagnosing a broken install

If `bundled-extensions` is present but `lstk-extensions.toml` is missing,
unreadable or empty, lstk cannot know which commands the binary provides.
Running a command the bundle would normally provide reports "bundled
extensions are not usable" with the reason, instead of "unknown command";
`lstk --help` still renders, without the bundled entries. Reinstalling from
the same channel restores the pair.
The same happens when the file names a command that cannot be dispatched (a
space, a path character, a leading hyphen): the whole bundle is reported as
unusable rather than partially loaded, because the file is a release artifact
and an inconsistency in it is a release bug, not a per-entry condition.

## Release-candidate checklist

Run on the first bundling release and after any packaging change. On each
channel:

- Fresh install (`curl` + `tar`; `brew install localstack/tap/lstk`;
  `npm install -g @localstack/lstk`), then `lstk <name>` runs immediately and
  `lstk --help` lists it with its description.
- On macOS, a bundled command runs with no Gatekeeper prompt from a genuinely
  downloaded archive.
- Homebrew has created a `bin` symlink for `lstk` only; the bundle is not
  symlinked.
- Starting from the previously released version, `lstk update` succeeds and
  the installed bundle matches the new lstk.
- The published release notes carry the `Bundled extensions:` line.
