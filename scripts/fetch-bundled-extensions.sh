#!/usr/bin/env bash
#
# Stages LocalStack's bundled extensions for a release build.
#
# Downloads one extensions bundle from the private extensions repository's
# release assets, verifies every asset against that release's checksum
# manifest, and arranges the contents under bundled/ in the layout the
# packaging step consumes:
#
#     bundled/<os>_<arch>/bundled-extensions[.exe]   one per platform
#     bundled/lstk-extensions.toml                   os/arch-independent
#
# The private repository publishes one archive per platform,
# `bundled-extensions_<tag>_<os>_<arch>.tar.gz` (`.zip` for Windows), each
# containing the multi-call binary `bundled-extensions[.exe]` and the
# descriptions file `lstk-extensions.toml`, plus a `checksums.txt` covering the
# archives.
#
# Only those two files are staged, and only they reach an lstk package: lstk
# dispatches to the one binary by argv[0] and takes its command list from the
# toml, so a per-command file on disk would serve no purpose on any channel.
#
# Which commands the binary actually provides is not this script's business —
# `bundled-extensions list` answers that, and scripts/check-descriptions.sh
# asks it. What this script does guarantee is that every archive carries an
# identical toml, so that answer can be checked against one file rather than
# six.
#
# Which bundle is taken comes from bundled/extensions.version — `latest` by
# default. `latest` is resolved to a concrete tag exactly once here and
# printed; every later step in the release uses that tag, so one build can
# never mix two bundles. Re-running an already-published release must pass that
# release's recorded tag back in via --tag / LSTK_EXTENSIONS_TAG, because
# `latest` re-resolves on every invocation.
#
# Usage:
#   scripts/fetch-bundled-extensions.sh [--tag <tag>] [--stub]
#
#   --tag <tag>   Use this bundle tag instead of resolving the version file.
#   --stub        Skip the download entirely and write placeholder files into
#                 the same layout, for local snapshot builds by contributors
#                 without access to the private repository.
#
# Environment:
#   LSTK_EXTENSIONS_READ_TOKEN   Read-only token scoped to the private
#                                extensions repository. Required (not --stub).
#   LSTK_EXTENSIONS_REPO         Private repository (default below).
#   LSTK_EXTENSIONS_TAG          Same as --tag.
#   LSTK_BUNDLED_DIR             Staging tree (default: <repo>/bundled).
#   LSTK_UNSUPPORTED_PLATFORMS   Overrides UNSUPPORTED_PLATFORMS below.
#   LSTK_BUNDLED_STUB_BINARIES   --stub only: binary names to fabricate.
set -euo pipefail

# The platforms lstk itself is built for (.goreleaser.yaml `builds`). Every
# bundled extension must have a binary for each of them.
TARGET_PLATFORMS="linux_amd64 linux_arm64 darwin_amd64 darwin_arm64 windows_amd64 windows_arm64"

# Platforms the bundle is knowingly not built for. Adding one here is how a gap
# becomes a deliberate, reviewable choice instead of a release failure; leaving
# it empty is what makes an accidental gap loud.
UNSUPPORTED_PLATFORMS="${LSTK_UNSUPPORTED_PLATFORMS-}"

REPO="${LSTK_EXTENSIONS_REPO:-localstack/lstk-bundled-extensions}"
BUNDLED_BINARY="bundled-extensions"
DESCRIPTIONS_FILE="lstk-extensions.toml"
MANIFEST_FILE="checksums.txt"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUNDLED_DIR="${LSTK_BUNDLED_DIR:-${REPO_ROOT}/bundled}"
VERSION_FILE="${BUNDLED_DIR}/extensions.version"

die() {
  echo "fetch-bundled-extensions: $*" >&2
  exit 1
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

is_unsupported_platform() {
  local candidate="$1" platform
  for platform in $(echo "${UNSUPPORTED_PLATFORMS}" | tr ',' ' '); do
    [ "${platform}" = "${candidate}" ] && return 0
  done
  return 1
}

# The version file is documentation as much as configuration, so comments and
# blank lines are ignored and exactly one value line is expected.
read_version_file() {
  [ -f "${VERSION_FILE}" ] || die "no version file at ${VERSION_FILE} (it selects which extensions bundle to ship)"
  local values
  values="$(sed -e 's/#.*//' -e 's/[[:space:]]*$//' -e 's/^[[:space:]]*//' "${VERSION_FILE}" | grep -v '^$' || true)"
  [ -n "${values}" ] || die "${VERSION_FILE} has no value line; expected 'latest' or a release tag"
  [ "$(echo "${values}" | wc -l | tr -d ' ')" -eq 1 ] || die "${VERSION_FILE} has more than one value line; expected exactly one"
  echo "${values}"
}

# Wipes the staging tree without touching the one tracked file in it, so a
# re-run never merges a previous bundle's binaries into the current one.
reset_staging_tree() {
  mkdir -p "${BUNDLED_DIR}"
  find "${BUNDLED_DIR}" -mindepth 1 -maxdepth 1 ! -name "$(basename "${VERSION_FILE}")" -exec rm -rf {} +
}

# Reads the platform out of an archive asset name:
# `bundled-extensions_v2026.08.19_windows_amd64.zip` -> `windows amd64 zip`.
# Returns non-zero when the name is not a platform archive.
split_asset_name() {
  local base="$1" stem kind arch rest os
  case "${base}" in
    *.tar.gz) stem="${base%.tar.gz}"; kind="tar.gz" ;;
    *.zip) stem="${base%.zip}"; kind="zip" ;;
    *) return 1 ;;
  esac
  case "${stem}" in *_*_*) ;; *) return 1 ;; esac
  arch="${stem##*_}"
  rest="${stem%_*}"
  os="${rest##*_}"
  case " ${TARGET_PLATFORMS} " in *" ${os}_${arch} "*) ;; *) return 1 ;; esac
  echo "${os} ${arch} ${kind}"
}

verify_checksums() {
  local dir="$1"
  local manifest="${dir}/${MANIFEST_FILE}"
  [ -f "${manifest}" ] || die "the bundle publishes no ${MANIFEST_FILE}; refusing to stage unverified binaries"
  local file base expected actual count=0
  for file in "${dir}"/*; do
    base="$(basename "${file}")"
    [ "${base}" = "${MANIFEST_FILE}" ] && continue
    # `*name` is the binary-mode form some sha256sum implementations emit.
    expected="$(awk -v f="${base}" '$2 == f || $2 == "*" f { print $1; exit }' "${manifest}")"
    [ -n "${expected}" ] || die "asset ${base} is not listed in ${MANIFEST_FILE}"
    actual="$(sha256_of "${file}")"
    [ "${actual}" = "${expected}" ] || die "checksum mismatch for ${base}: manifest says ${expected}, downloaded file is ${actual}"
    count=$((count + 1))
  done
  echo "Verified ${count} asset(s) against ${MANIFEST_FILE}."
}

# Unpacks one archive into an empty directory. Whatever else it holds is
# ignored: stage_assets looks up the binary and the toml by name and copies only
# those two.
extract_archive() {
  local archive="$1" kind="$2" dest="$3"
  mkdir -p "${dest}"
  case "${kind}" in
    tar.gz) tar xzf "${archive}" -C "${dest}" ;;
    zip) unzip -q -o "${archive}" -d "${dest}" ;;
  esac
}

stage_assets() {
  local dir="$1" file base parsed os arch kind ext unpacked binary toml staged=0
  local toml_staged="${BUNDLED_DIR}/${DESCRIPTIONS_FILE}"
  for file in "${dir}"/*; do
    base="$(basename "${file}")"
    [ "${base}" = "${MANIFEST_FILE}" ] && continue
    if ! parsed="$(split_asset_name "${base}")"; then
      echo "Note: ignoring release asset that is not a platform archive: ${base}"
      continue
    fi
    # shellcheck disable=SC2086 # deliberate word splitting of the parsed tuple
    set -- ${parsed}
    os="$1"; arch="$2"; kind="$3"
    ext=""
    [ "${os}" = "windows" ] && ext=".exe"

    unpacked="${dir}/.unpacked/${os}_${arch}"
    extract_archive "${file}" "${kind}" "${unpacked}"

    binary="$(find "${unpacked}" -type f -name "${BUNDLED_BINARY}${ext}" | head -n1)"
    [ -n "${binary}" ] || die "${base} contains no ${BUNDLED_BINARY}${ext}"
    mkdir -p "${BUNDLED_DIR}/${os}_${arch}"
    cp "${binary}" "${BUNDLED_DIR}/${os}_${arch}/${BUNDLED_BINARY}${ext}"
    chmod 0755 "${BUNDLED_DIR}/${os}_${arch}/${BUNDLED_BINARY}${ext}"
    staged=$((staged + 1))

    # The descriptions file is os/arch-independent: take it from the first
    # archive and insist every other archive agrees, since a bundle whose
    # platforms describe different commands is a bug in the bundle.
    toml="$(find "${unpacked}" -type f -name "${DESCRIPTIONS_FILE}" | head -n1)"
    [ -n "${toml}" ] || die "${base} contains no ${DESCRIPTIONS_FILE}"
    if [ -f "${toml_staged}" ]; then
      cmp -s "${toml}" "${toml_staged}" || die "${DESCRIPTIONS_FILE} in ${base} differs from the one in an earlier archive of the same bundle"
    else
      cp "${toml}" "${toml_staged}"
    fi
  done
  [ -f "${toml_staged}" ] || die "the bundle publishes no ${DESCRIPTIONS_FILE}"
  echo "Staged ${staged} platform binaries into ${BUNDLED_DIR}."
}

# The bundle must exist for every non-exempt target platform. Checking here
# turns a gap into a named error at pull time; left to GoReleaser it surfaces
# as an unexplained "glob matched nothing" during the release.
check_platform_coverage() {
  local names platform name file suffix missing=""
  names="$(find "${BUNDLED_DIR}" -mindepth 2 -maxdepth 2 -type f -exec basename {} \; \
    | sed 's/\.exe$//' | sort -u)"
  if [ -z "${names}" ]; then
    echo "Warning: the bundle staged no extension binaries."
    return 0
  fi
  for platform in ${TARGET_PLATFORMS}; do
    if is_unsupported_platform "${platform}"; then
      echo "Note: ${platform} is listed as unsupported; skipping its coverage check."
      continue
    fi
    suffix=""
    case "${platform}" in windows_*) suffix=".exe" ;; esac
    for name in ${names}; do
      file="${BUNDLED_DIR}/${platform}/${name}${suffix}"
      [ -f "${file}" ] || missing="${missing}  ${name} for ${platform}
"
    done
  done
  if [ -n "${missing}" ]; then
    echo "fetch-bundled-extensions: the bundle has no binary for:" >&2
    printf '%s' "${missing}" >&2
    die "add the missing platforms to the bundle, or list them in UNSUPPORTED_PLATFORMS"
  fi
  echo "Platform coverage complete for: $(echo "${names}" | tr '\n' ' ')"
}

write_stub_bundle() {
  # Defaults to the real layout so `goreleaser --snapshot` works out of the
  # box; the override exists for experiments with standalone lstk-<name> files.
  local binaries="${LSTK_BUNDLED_STUB_BINARIES:-${BUNDLED_BINARY}}"
  local platform name suffix key
  reset_staging_tree
  for platform in ${TARGET_PLATFORMS}; do
    is_unsupported_platform "${platform}" && continue
    suffix=""
    case "${platform}" in windows_*) suffix=".exe" ;; esac
    mkdir -p "${BUNDLED_DIR}/${platform}"
    for name in ${binaries}; do
      if [ "${name}" = "${BUNDLED_BINARY}" ]; then
        # The stub answers `list` like the real bundle does, so the descriptions
        # gate can be run against a stub tree.
        printf '#!/bin/sh\nif [ "$1" = "list" ]; then echo "doctor"; exit 0; fi\necho "stub %s for %s"\n' \
          "${name}" "${platform}" > "${BUNDLED_DIR}/${platform}/${name}${suffix}"
      else
        printf '#!/bin/sh\necho "stub %s for %s"\n' "${name}" "${platform}" \
          > "${BUNDLED_DIR}/${platform}/${name}${suffix}"
      fi
      chmod 0755 "${BUNDLED_DIR}/${platform}/${name}${suffix}"
    done
  done
  {
    echo "# Stub descriptions file written by fetch-bundled-extensions.sh --stub."
    echo "# The real one is hand-authored in the private extensions repository."
    for name in ${binaries}; do
      case "${name}" in
        "${BUNDLED_BINARY}")
          # The multi-call bundle: describe one placeholder command so lstk
          # has something to dispatch to it.
          echo "doctor = \"Stub bundled command (local build only)\""
          ;;
        lstk-*)
          key="${name#lstk-}"
          echo "${key} = \"Stub description for ${key} (local build only)\""
          ;;
      esac
    done
  } > "${BUNDLED_DIR}/${DESCRIPTIONS_FILE}"

  cat >&2 <<'BANNER'

  ##########################################################
  #                                                        #
  #  STUB BUNDLE: placeholder files, not real extensions.  #
  #                                                        #
  #  For local snapshot builds only. Artifacts built from  #
  #  this bundle must never be released.                   #
  #                                                        #
  ##########################################################

BANNER
}

STUB=0
TAG="${LSTK_EXTENSIONS_TAG-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --stub) STUB=1 ;;
    --tag)
      [ $# -ge 2 ] || die "--tag needs a value"
      TAG="$2"
      shift
      ;;
    --tag=*) TAG="${1#--tag=}" ;;
    -h|--help)
      sed -n '2,45p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) die "unknown argument: $1" ;;
  esac
  shift
done

if [ "${STUB}" -eq 1 ]; then
  write_stub_bundle
  echo "Staged a stub bundle into ${BUNDLED_DIR}."
  exit 0
fi

if [ -z "${LSTK_EXTENSIONS_READ_TOKEN-}" ]; then
  die "LSTK_EXTENSIONS_READ_TOKEN is not set.
  It must be a read-only token scoped to ${REPO}; in CI it comes from the
  repository secret of the same name. For a local snapshot build without
  access to that repository, re-run with --stub instead."
fi
command -v gh >/dev/null 2>&1 || die "the GitHub CLI (gh) is required to download the bundle"
command -v unzip >/dev/null 2>&1 || die "unzip is required to unpack the Windows bundle archives"

if [ -z "${TAG}" ]; then
  VERSION="$(read_version_file)"
  if [ "${VERSION}" = "latest" ]; then
    TAG="$(GH_TOKEN="${LSTK_EXTENSIONS_READ_TOKEN}" gh release view \
      --repo "${REPO}" --json tagName --jq .tagName)" \
      || die "could not resolve 'latest' to a release of ${REPO}"
    [ -n "${TAG}" ] || die "${REPO} has no published release to resolve 'latest' to"
  else
    TAG="${VERSION}"
  fi
fi

# Printed, not just logged: this is the one line every later release step and
# the published release notes need in order to pin the build to one bundle.
echo "Resolved extensions bundle: ${TAG} (${REPO})"

DOWNLOAD_DIR="$(mktemp -d)"
trap 'rm -rf "${DOWNLOAD_DIR}"' EXIT

GH_TOKEN="${LSTK_EXTENSIONS_READ_TOKEN}" gh release download "${TAG}" \
  --repo "${REPO}" --dir "${DOWNLOAD_DIR}" --clobber \
  || die "could not download release ${TAG} from ${REPO}"

verify_checksums "${DOWNLOAD_DIR}"
reset_staging_tree
stage_assets "${DOWNLOAD_DIR}"
check_platform_coverage

echo "Bundle ${TAG} staged in ${BUNDLED_DIR}."
