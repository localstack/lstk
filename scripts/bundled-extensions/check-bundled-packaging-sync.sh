#!/usr/bin/env bash
#
# Fails when the two halves of bundled-extension packaging are out of step.
#
# The packaging half (.goreleaser.yaml referencing bundled/) and the download
# half (the release job running fetch-bundled-extensions.sh) must land in the
# same PR. Packaging without the download makes every release fail on a glob
# that matches nothing; the download without packaging silently ships nothing.
#
# The build flag internal/version.bundlesExtensions (an ldflag in the same
# .goreleaser.yaml) is the third half: it tells a release binary to expect the
# bundle beside it and gates the reinstall hints. It must be `true` exactly when
# packaging is live, or the hints stay silent on a release that ships a bundle
# (or fire on one that does not).
#
# The bundle's licence documents are the fourth half: the fetch script stages
# them (BUNDLE_DOCUMENTS, bare archive names), .goreleaser.yaml ships the
# staged names, add-bundled-to-npm.sh copies the same staged names into the npm
# packages, and internal/update installs them on `lstk update`. The first three
# must agree exactly; the updater may name more (a document nobody ships yet),
# never fewer.
#
# `goreleaser check` cannot catch any of this: it validates config syntax and
# never looks at the filesystem or at the workflow. So this runs beside it on
# every PR, where it costs a red build instead of a broken release.
#
# Usage:
#   scripts/bundled-extensions/check-bundled-packaging-sync.sh \
#     [goreleaser.yaml] [ci-workflow.yml] [fetch script] [npm script] [extract.go]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

GORELEASER_FILE="${1:-${REPO_ROOT}/.goreleaser.yaml}"
WORKFLOW_FILE="${2:-${REPO_ROOT}/.github/workflows/ci.yml}"
FETCH_FILE="${3:-${SCRIPT_DIR}/fetch-bundled-extensions.sh}"
NPM_FILE="${4:-${SCRIPT_DIR}/add-bundled-to-npm.sh}"
UPDATE_FILE="${5:-${REPO_ROOT}/internal/update/extract.go}"

FETCH_SCRIPT="fetch-bundled-extensions.sh"
STAGING_DIR="bundled/"
BUNDLE_FLAG="internal/version.bundlesExtensions"

die() {
  echo "check-bundled-packaging-sync: $*" >&2
  exit 1
}

[ -f "${GORELEASER_FILE}" ] || die "no such file: ${GORELEASER_FILE}"
[ -f "${WORKFLOW_FILE}" ] || die "no such file: ${WORKFLOW_FILE}"
[ -f "${FETCH_FILE}" ] || die "no such file: ${FETCH_FILE}"
[ -f "${NPM_FILE}" ] || die "no such file: ${NPM_FILE}"
[ -f "${UPDATE_FILE}" ] || die "no such file: ${UPDATE_FILE}"

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

flag_on=0
if uncommented "${GORELEASER_FILE}" | grep -q -- "${BUNDLE_FLAG}=true"; then
  flag_on=1
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

if [ "${packaging_live}" -eq 1 ] && [ "${flag_on}" -eq 0 ]; then
  die "$(basename "${GORELEASER_FILE}") packages files from ${STAGING_DIR}, but its
  ldflags do not stamp ${BUNDLE_FLAG}=true. Release binaries would not know a
  bundle ships beside them, so the reinstall hints for a missing bundle would
  never show. Set the flag to true."
fi

if [ "${packaging_live}" -eq 0 ] && [ "${flag_on}" -eq 1 ]; then
  die "$(basename "${GORELEASER_FILE}") stamps ${BUNDLE_FLAG}=true, but packages
  nothing from ${STAGING_DIR}. Every release binary would report its bundle as
  missing. Set the flag to false, or enable the packaging entries."
fi

# The value of a `NAME="a b c"` assignment in a shell script.
shell_list() {
  sed -n "s/^$2=\"\(.*\)\"\$/\1/p" "$1" | head -n1
}

# Whether the space-separated list $1 contains the word $2.
has_word() {
  case " $1 " in *" $2 "*) return 0 ;; esac
  return 1
}

check_document_lists() {
  local prefix fetch_docs staged_from_fetch shipped npm_docs name
  prefix="$(shell_list "${FETCH_FILE}" BUNDLED_BINARY)"
  prefix="${prefix:-bundled-extensions}"
  fetch_docs="$(shell_list "${FETCH_FILE}" BUNDLE_DOCUMENTS)"
  staged_from_fetch=""
  for name in ${fetch_docs}; do staged_from_fetch="${staged_from_fetch} ${prefix}.${name}"; done
  # `src: bundled/<prefix>.<name>` entries; the per-platform binary glob has an
  # os/arch directory in between and so never matches.
  shipped="$(uncommented "${GORELEASER_FILE}" \
    | sed -n "s|^-* *src: *[\"']*bundled/\(${prefix}\.[^\"' ]*\).*|\1|p" | tr '\n' ' ')"
  npm_docs="$(shell_list "${NPM_FILE}" STAGED_BUNDLE_DOCUMENTS)"

  for name in ${staged_from_fetch}; do
    has_word "${shipped}" "${name}" || die "$(basename "${FETCH_FILE}") stages ${name}, but
  $(basename "${GORELEASER_FILE}") has no archives.files entry for bundled/${name}.
  The document would be downloaded and then left out of every archive."
  done
  for name in ${shipped}; do
    has_word "${staged_from_fetch}" "${name}" || die "$(basename "${GORELEASER_FILE}") ships bundled/${name}, but
  $(basename "${FETCH_FILE}") never stages it (BUNDLE_DOCUMENTS). Every release
  would fail on a file that does not exist."
    has_word "${npm_docs}" "${name}" || die "$(basename "${GORELEASER_FILE}") ships ${name}, but
  $(basename "${NPM_FILE}") does not copy it (STAGED_BUNDLE_DOCUMENTS). The npm
  platform packages would ship the proprietary bundle without it."
    grep -q "\"${name}\"" "${UPDATE_FILE}" || die "$(basename "${GORELEASER_FILE}") ships ${name}, but
  $(basename "${UPDATE_FILE}") does not install it, so \`lstk update\` would leave
  it stale beside a new bundle."
  done
  for name in ${npm_docs}; do
    has_word "${shipped}" "${name}" || die "$(basename "${NPM_FILE}") copies ${name}, but
  $(basename "${GORELEASER_FILE}") does not ship it, so the fetch step never
  stages it and the npm step would fail on a missing file."
  done
}

if [ "${packaging_live}" -eq 1 ]; then
  check_document_lists
  echo "In step: bundled extensions are downloaded by the release job, packaged, and the build flag is on."
  echo "In step: the bundle's licence documents are staged, shipped, copied to npm and installed by the updater."
else
  echo "In step: bundled-extension packaging is not enabled, nothing downloads it, and the build flag is off."
fi
