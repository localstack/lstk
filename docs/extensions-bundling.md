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

## How `lstk <name>` is resolved

In order, first match wins:

1. **A built-in command or alias** (`lstk start`, `lstk aws`, …). Built-ins
   always win, so a bundle can never shadow one.
2. **The bundle.** If `bundled-extensions` sits next to lstk and
   `lstk-extensions.toml` lists `<name>`, lstk runs that binary with `argv[0]`
   set to `lstk-<name>`.
3. **A standalone `lstk-<name>` executable**, first next to lstk, then on
   `PATH` — the third-party extension path, unchanged by bundling.

If nothing matches, `lstk <name>` reports an unknown command exactly as it did
before bundling existed. The bundle is consulted only through the toml, so a
name the toml does not list is indistinguishable from a typo — which is the
intent. The one case that is not "unknown command" is a bundle that is
installed but whose toml cannot be read; see
[Diagnosing a broken install](#diagnosing-a-broken-install).

## Where the files live on disk

lstk looks in one place: the directory of its own symlink-resolved executable
(`extension.BundledDir`). Each install method lands the two files there
without any layout work of its own.

| Channel | Directory | How it gets there |
| --- | --- | --- |
| Binary archive (`curl` + `tar`) | Wherever the user extracted the archive; the files sit at the archive root next to `lstk`. | GoReleaser `archives.files` entries in `.goreleaser.yaml`. |
| Homebrew | The cask's Caskroom staged directory, e.g. `/opt/homebrew/Caskroom/lstk/<version>/`. `bin/lstk` is a symlink into it; lstk resolves the link. | The cask stages the whole archive. Only `lstk` is symlinked into `bin`; the bundle is found via the directory, never via `PATH`. The post-install hook strips the macOS quarantine attribute from the **whole** staged directory so the bundle runs without a Gatekeeper prompt. |
| npm | The **platform** package, e.g. `node_modules/@localstack/lstk_darwin_arm64/` (underscores; the wrapper `@localstack/lstk` holds only the launcher). The launcher execs the Go binary from there, so that is where lstk's bundled dir resolves to. | `scripts/add-bundled-to-npm.sh` copies the files into each platform package under `dist/npm/` before `npm publish`, **and** adds them to that package's `files` allowlist. The publisher generates `"files": []`, which npm reads as "only `package.json` and the `bin` entry", so a plain copy would be silently dropped at publish. Its output directories are slugified (`dist/npm/lstk-darwin-arm-64-v-8-0`) and cannot be parsed back into a platform, so the script reads the authoritative `@localstack/lstk_<goos>_<goarch>` name from each `package.json` instead. |

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
   archive must carry an identical copy, which is what lets the next step
   verify one file rather than six). Nothing else is staged, and nothing else
   reaches a released package. It fails if any of lstk's six target platforms
   (`linux`/`darwin`/`windows` × `amd64`/`arm64`) has no archive. A platform can be exempted only by listing it in
   `UNSUPPORTED_PLATFORMS` at the top of the script, so a gap is always a
   visible choice.
3. **Gate the pairing.** `scripts/check-descriptions.sh bundled/linux_amd64`
   reads the command names from the toml (left-hand side only; values are
   never parsed) and compares them with the bundle's own answer: it runs
   `bundled-extensions list`, which prints one bare command name per line. It
   fails if commands are described but there is no binary, if there is a
   binary but no commands are described, if `list` fails or prints nothing or
   prints something that is not a command name, or if the toml describes a
   command the bundle does not provide (lstk would exec the binary under a
   name it does not answer to). A command the bundle provides but the toml
   omits only warns: lstk will not expose it.

   Because it runs the binary, the gate can only be pointed at a platform
   directory the host can execute — `linux_amd64` on the release runner — so
   only that platform's binary is interrogated. A bundle whose platforms
   disagree with each other is outside what this can see; the identical-toml
   check in step 2 is what makes one directory a fair sample.
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
  Windows) and `lstk-extensions.toml`. Anything else in the archive is ignored;
  only those two files are staged and shipped;
- a `list` subcommand on the binary: `bundled-extensions list` prints the
  commands it provides, one bare name per line and nothing else (each matching
  `^[A-Za-z0-9][A-Za-z0-9_-]*$`), exiting zero. This is the binary's own
  statement of what it answers to, and the release gate checks the toml
  against it. A different output shape fails the release rather than being
  parsed loosely, so that a change here surfaces as a red build and not as a
  silently shorter command list;
- the same `lstk-extensions.toml` in every archive, hand-authored, describing
  every command the binary provides — a described command with no
  implementation would show in `lstk --help` and fail when run;
- `checksums.txt` with a SHA-256 line for every archive.

The binary must also **fail helpfully when it is run outside lstk**. lstk sets
`LSTK_EXT_API_VERSION` and `LSTK_EXT_CONTEXT` before executing it; run from a
shell, neither is present and the binary has no config directory, no auth
token and no emulator list to work with. `bundled-extensions` sits in the
install directory next to `lstk`, so someone will eventually find it and run
it. When `LSTK_EXT_API_VERSION` is unset it should print a short message
saying it is part of lstk and naming the command to use instead (for example
`lstk doctor`), and exit non-zero — not crash, and not half-run against
defaults.

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

It exists because the `bundled/` entries in `.goreleaser.yaml` are live: with
an empty staging tree `goreleaser` fails outright, so without `--stub` nobody
without a private-repo credential could run a snapshot build or work on the
packaging at all. It makes the packaging path runnable — it is not a way to
test the extensions, and the real bundle is what the release-candidate
checklist below exercises.

`scripts/check-bundled-packaging-sync.sh` runs on every PR next to
`goreleaser check`. It fails if `.goreleaser.yaml` references `bundled/` while
the release job has no fetch step, or the reverse. `goreleaser check` cannot
catch this itself because it only validates config syntax and never looks at
the filesystem. Both halves must land in the same PR.

The bash suites for all four scripts run with `make test-scripts`.

## Testing a release candidate before publishing

A snapshot build produces the same artifacts the real release publishes, so all
three channels can be installed and exercised before anything reaches GitHub,
Homebrew or npm. Stage a bundle and build once:

```bash
LSTK_EXTENSIONS_READ_TOKEN=<token> scripts/fetch-bundled-extensions.sh
goreleaser release --snapshot --clean
```

Use the real bundle here, not `--stub`: the stub only proves the packaging path
runs, and says nothing about the extensions themselves.

### Binary archive

The archives in `dist/` are the ones the release uploads, so there is nothing
further to do.

```bash
mkdir -p /tmp/lstk-rc && tar xzf dist/lstk_<version>_darwin_arm64.tar.gz -C /tmp/lstk-rc
/tmp/lstk-rc/lstk doctor
```

### npm

Run the three steps the release job runs, in order:

```bash
npx --yes goreleaser-npm-publisher@1.5.0 build --project . --prefix @localstack \
  --license Apache-2.0 \
  --description "LocalStack CLI v2 - Start and manage LocalStack emulators" \
  --files README.md LICENSE
cp npm/launcher.js dist/npm/lstk/index.js
scripts/add-bundled-to-npm.sh dist/npm bundled
```

Then **pack the packages before installing them**. `ls dist/npm/` and pick the
slug for your own platform:

```bash
npm pack --pack-destination /tmp/lstk-tgz \
  ./dist/npm/lstk-darwin-arm-64-v-8-0 ./dist/npm/lstk
mkdir -p /tmp/lstk-npm && cd /tmp/lstk-npm
npm install --no-save /tmp/lstk-tgz/*.tgz
./node_modules/.bin/lstk doctor
```

Installing the directories directly (`npm install ./dist/npm/lstk`) does not
work: npm symlinks a local directory, so the launcher's `__dirname` resolves
outside the install tree and it reports `no prebuilt binary found for
<platform>`. That is an artifact of installing from a path, not a packaging
fault — after `npm pack` the layout is identical to a registry install.

### Homebrew cask

GoReleaser writes the cask it would push to the tap at
`dist/homebrew/Casks/lstk.rb`, with `sha256` values already matching the local
archives. Two changes make it installable: point the urls at the local files,
and put it in a tap, because Homebrew refuses a loose `.rb` path.

```bash
sed "s#https://github.com/localstack/lstk/releases/download/v[^/]*/#file://${PWD}/dist/#" \
  dist/homebrew/Casks/lstk.rb > /tmp/lstk.rb
brew tap-new localstack/rc-test
cp /tmp/lstk.rb "$(brew --repository)/Library/Taps/localstack/homebrew-rc-test/Casks/"
brew install --cask localstack/rc-test/lstk
```

Undo with `brew uninstall --cask localstack/rc-test/lstk` followed by
`brew untap localstack/rc-test`. This is the only way to check the two cask
properties that matter before release: that `lstk` alone is symlinked into
`bin`, and that a bundled command runs with no Gatekeeper prompt.

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

Run on the first bundling release and after any packaging change. Do it against
a real release candidate before publishing, using the locally built artifacts
from [Testing a release candidate before
publishing](#testing-a-release-candidate-before-publishing); the fresh-install
commands below are the published equivalents. On each channel:

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
