#!/usr/bin/env bash
# Render the Homebrew cask and check what a release would publish.
#
# Only a release writes the cask, into a tap with no CI, so a fault reaches
# users as their next `brew install` -- how the deprecated `postflight` stanza
# shipped (localstack/lstk#501, localstack/homebrew-tap#7).
#
# Homebrew's own linters need Homebrew, absent on the Linux runners, so they run
# against this output on macOS -- scripts/lint-cask.sh.
#
# Wipes dist/ and stubs bundled/; both paths come from the config.

set -euo pipefail

cd "$(dirname "$0")/.."

CASK=dist/homebrew/Casks/lstk.rb

if ! command -v goreleaser >/dev/null 2>&1; then
  echo "goreleaser required on PATH: https://goreleaser.com/install/" >&2
  exit 1
fi

# The renderer's version is part of the artifact's definition, as `make lint`
# treats golangci-lint.
EXPECTED=$(awk '/^goreleaser/ {print $2}' .tool-versions)
INSTALLED=$(goreleaser --version 2>/dev/null | awk '/GitVersion:/ {print $2}' | sed 's/^v//')
if [ "$INSTALLED" != "$EXPECTED" ]; then
  echo "goreleaser $EXPECTED required (found: ${INSTALLED:-none})" >&2
  exit 1
fi

# archives.files globs `bundled/`, so an empty tree fails the build. The cask
# ignores the bundle's contents and the real fetch needs a private-repo token,
# so stub it.
scripts/bundled-extensions/fetch-bundled-extensions.sh --stub

goreleaser release --snapshot --clean

[ -f "$CASK" ] || { echo "no cask generated at $CASK" >&2; exit 1; }

fail() { echo "FAIL: $1" >&2; echo "--- $CASK ---" >&2; cat "$CASK" >&2; exit 1; }

# Homebrew warns on every `brew` operation that loads a raw-Ruby flight block,
# and points the user at our tap. Matches only the deprecated spellings --
# `_steps` leaves no trailing ` do` -- and ignores indentation, so an upstream
# reindent cannot silently empty the check.
grep -Eq '^[[:space:]]*(uninstall_)?(pre|post)flight do$' "$CASK" &&
  fail "cask uses a deprecated raw-Ruby flight stanza; use the *_steps form"

# Our darwin binaries are unsigned, so Gatekeeper kills them unless the cask
# clears quarantine. Deleting the hook would satisfy the check above, so assert
# the step survives too.
grep -q 'postflight_steps do' "$CASK" ||
  fail "cask has no postflight_steps stanza"
grep -q 'com.apple.quarantine' "$CASK" ||
  fail "cask no longer clears com.apple.quarantine"

# Homebrew's tokens share GoReleaser's delimiters, so `{{staged_path}}` must
# survive the template pass. An unescaped token fails the release; a broken
# escape would emit a literal that resolves to the wrong path.
grep -q '"{{staged_path}}"' "$CASK" ||
  fail "quarantine step does not target the whole staged dir"

echo "OK: $CASK"
