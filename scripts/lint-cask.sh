#!/usr/bin/env bash
# Run Homebrew's own linters over a generated cask.
#
# check-cask.sh asserts what we know to look for; these cops catch what we did
# not -- `Cask/InstallSteps` is what flags localstack/lstk#501. They need
# Homebrew, absent on the Linux runners, so CI renders there and lints here.
#
# Usage: scripts/lint-cask.sh [path/to/lstk.rb]

set -euo pipefail

cd "$(dirname "$0")/.."

CASK="${1:-dist/homebrew/Casks/lstk.rb}"

[ -f "$CASK" ] || { echo "no cask at $CASK -- run 'make check-cask' first" >&2; exit 1; }

if ! command -v brew >/dev/null 2>&1; then
  echo "brew required on PATH: https://brew.sh" >&2
  exit 1
fi

export HOMEBREW_NO_AUTO_UPDATE=1

# `brew audit` refuses a bare path, and the cask cops only fire under `Casks/`,
# so this needs a tap. Build a throwaway one and drop it however we exit.
TAP=lstkci/caskcheck
untap() { brew untap "$TAP" >/dev/null 2>&1 || true; }
trap untap EXIT
untap
brew tap-new --no-git "$TAP" >/dev/null
TAP_CASKS="$(brew --repository "$TAP")/Casks"
mkdir -p "$TAP_CASKS"
cp "$CASK" "$TAP_CASKS/lstk.rb"

# TODO(#512): drop --except-cops once hooks.post.install_steps replaces
# custom_block, which renders the stanza first and trips Cask/StanzaOrder on
# everything after it. Scoped to that cop so the deprecation cops still fire.
brew style --except-cops=Cask/StanzaOrder "$TAP_CASKS/lstk.rb"

# Passes on a deprecated stanza, so this guards other faults, not #501. Its
# `--online` form checks urls and checksums, but only the tap has a published
# release to check against.
brew audit --cask "$TAP/lstk"

echo "OK: Homebrew linters clean for $CASK"
