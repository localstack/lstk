# shellcheck shell=sh disable=SC2034 # defines variables for the scripts that source it
# Shared by .spiral/check.sh and the stage scripts under .spiral/checks/, as
#   . "$(dirname "$0")/lib.sh"
# Leaves the caller at the repo root with setup.sh's tools on PATH (every run
# gets a fresh environment) and defines the `step` banner. POSIX sh.
set -eu

root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$root"
grep -qx 'module github.com/localstack/lstk' go.mod 2>/dev/null || {
  echo "checks: not at the lstk repo root ($root)" >&2
  exit 1
}

[ -d /usr/local/go/bin ] && export PATH="/usr/local/go/bin:$PATH"
export PATH="/usr/local/bin:$PATH"
export OPENSPEC_TELEMETRY=0

# Spiral commits the implementer's changes before running the gate, so the
# branch, not the working tree, is what a stage judges.
base=origin/main
range="$base...HEAD"

step() { echo ""; echo "=== checks: $* ==="; }
