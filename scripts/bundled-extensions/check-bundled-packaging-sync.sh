#!/usr/bin/env bash
#
# Fails when the two halves of bundled-extension packaging are out of step.
#
# The packaging half (.goreleaser.yaml referencing bundled/) and the download
# half (the release job running fetch-bundled-extensions.sh) must land in the
# same PR. Packaging without the download makes every release fail on a glob
# that matches nothing; the download without packaging silently ships nothing.
#
# `goreleaser check` cannot catch either direction: it validates config syntax
# and never looks at the filesystem or at the workflow. So this runs beside it
# on every PR, where it costs a red build instead of a broken release.
#
# Usage:
#   scripts/bundled-extensions/check-bundled-packaging-sync.sh [goreleaser.yaml] [ci-workflow.yml]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

GORELEASER_FILE="${1:-${REPO_ROOT}/.goreleaser.yaml}"
WORKFLOW_FILE="${2:-${REPO_ROOT}/.github/workflows/ci.yml}"

FETCH_SCRIPT="fetch-bundled-extensions.sh"
STAGING_DIR="bundled/"

die() {
  echo "check-bundled-packaging-sync: $*" >&2
  exit 1
}

[ -f "${GORELEASER_FILE}" ] || die "no such file: ${GORELEASER_FILE}"
[ -f "${WORKFLOW_FILE}" ] || die "no such file: ${WORKFLOW_FILE}"

# Drops comment lines so a commented-out entry never counts as live. YAML has
# no block comments, so line-wise is exact here.
uncommented() {
  sed -e 's/^[[:space:]]*//' "$1" | grep -v '^#' || true
}

# The steps of the `release` job only — a fetch step in some other job does not
# populate the staging tree for the release.
release_job_steps() {
  awk '
    /^  release:[[:space:]]*$/ { in_job = 1; next }
    in_job && /^  [A-Za-z_][A-Za-z0-9_-]*:/ { in_job = 0 }
    in_job { print }
  ' "$1" | sed -e 's/^[[:space:]]*//' | grep -v '^#' || true
}

packaging_live=0
if uncommented "${GORELEASER_FILE}" | grep -q -- "${STAGING_DIR}"; then
  packaging_live=1
fi

fetch_wired=0
if release_job_steps "${WORKFLOW_FILE}" | grep -q -- "${FETCH_SCRIPT}"; then
  fetch_wired=1
fi

if [ "${packaging_live}" -eq 1 ] && [ "${fetch_wired}" -eq 0 ]; then
  die "$(basename "${GORELEASER_FILE}") packages files from ${STAGING_DIR}, but the
  release job in $(basename "${WORKFLOW_FILE}") never runs ${FETCH_SCRIPT}.
  Nothing would populate ${STAGING_DIR}, so every release would fail on a glob
  that matches no files. Add the fetch step, or comment the packaging entries
  back out."
fi

if [ "${packaging_live}" -eq 0 ] && [ "${fetch_wired}" -eq 1 ]; then
  die "the release job in $(basename "${WORKFLOW_FILE}") runs ${FETCH_SCRIPT}, but
  $(basename "${GORELEASER_FILE}") packages nothing from ${STAGING_DIR}.
  The bundle would be downloaded and then silently dropped. Add the packaging
  entries, or remove the fetch step."
fi

if [ "${packaging_live}" -eq 1 ]; then
  echo "In step: bundled extensions are downloaded by the release job and packaged."
else
  echo "In step: bundled-extension packaging is not enabled, and nothing downloads it."
fi
