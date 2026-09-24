#!/usr/bin/env bash
#
# Adds the bundled extensions and their licence documents to every npm
# PLATFORM package, and relabels those packages' licence.
#
# The npm wrapper (@localstack/lstk) only holds the launcher; the real Go
# binary lives in the platform package (@localstack/lstk_<goos>_<goarch>) and the
# launcher execs it from there. lstk resolves its bundled-extensions directory
# from its own executable's location, so that platform directory is where
# `bundled-extensions`, `lstk-extensions.toml` and the bundle's licence
# documents must live. The bundle is proprietary, so the platform packages
# cannot be labelled plain Apache-2.0 the way the publisher labels every
# package; the wrapper, which ships no bundle, keeps the publisher's label.
#
# goreleaser-npm-publisher has no per-platform extra-files option, so this runs
# on its dist/npm output before `npm publish`. Two details it has to get right:
#
#   * The publisher slugifies its output directory names
#     (dist/npm/lstk-darwin-arm-64-v-8-0), so a directory name cannot be parsed
#     back into a platform. The package.json inside carries the authoritative
#     name, @localstack/lstk_<goos>_<goarch>, which maps straight onto the
#     staging directory - so that is what this reads.
#   * npm packs only what package.json "files" lists (plus package.json and the
#     bin entry). Copying alone would be silently dropped at publish, so the
#     copied names are appended to that allowlist as well.
#
# Usage:
#   scripts/bundled-extensions/add-bundled-to-npm.sh <dist/npm dir> <bundled dir>
set -euo pipefail

# The licence of a package that ships both the Apache-2.0 core and the
# proprietary bundle. The one place the identifier lives; the exact
# LicenseRef is pending legal's confirmation.
PLATFORM_LICENSE="Apache-2.0 AND LicenseRef-LocalStack-Proprietary"

# The bundle's licence documents under the names fetch-bundled-extensions.sh
# stages them as (its BUNDLE_DOCUMENTS, prefixed). .goreleaser.yaml lists the
# same files; check-bundled-packaging-sync.sh fails when the two disagree.
STAGED_BUNDLE_DOCUMENTS="bundled-extensions.THIRD_PARTY_NOTICES.txt"

die() {
  echo "add-bundled-to-npm: $*" >&2
  exit 1
}

usage() {
  awk 'NR > 1 && !/^#/ { exit } NR > 1 { sub(/^# ?/, ""); print }' "${BASH_SOURCE[0]}" >&2
  exit 1
}

[ $# -eq 2 ] || usage
NPM_DIR="$1"
BUNDLED_DIR="$2"
TOML="${BUNDLED_DIR}/lstk-extensions.toml"

[ -d "${NPM_DIR}" ] || die "no such directory: ${NPM_DIR}"
[ -d "${BUNDLED_DIR}" ] || die "no such directory: ${BUNDLED_DIR}"
[ -f "${TOML}" ] || die "no descriptions file at ${TOML}; run scripts/bundled-extensions/fetch-bundled-extensions.sh first"
for doc in ${STAGED_BUNDLE_DOCUMENTS}; do
  [ -f "${BUNDLED_DIR}/${doc}" ] || die "no ${doc} at ${BUNDLED_DIR}; run scripts/bundled-extensions/fetch-bundled-extensions.sh first"
done
command -v node >/dev/null 2>&1 || die "node is required to edit package.json"

# Appends names to the package.json "files" array, de-duplicated, preserving
# everything else. Done in node so the JSON is rewritten faithfully.
register_files() {
  local pkg_json="$1"; shift
  node -e '
    const fs = require("fs");
    const [file, ...names] = process.argv.slice(1);
    const pkg = JSON.parse(fs.readFileSync(file, "utf8"));
    const files = Array.isArray(pkg.files) ? pkg.files : [];
    for (const name of names) if (!files.includes(name)) files.push(name);
    pkg.files = files;
    fs.writeFileSync(file, JSON.stringify(pkg, null, 2) + "\n");
  ' "${pkg_json}" "$@"
}

set_license() {
  node -e '
    const fs = require("fs");
    const [file, license] = process.argv.slice(1);
    const pkg = JSON.parse(fs.readFileSync(file, "utf8"));
    pkg.license = license;
    fs.writeFileSync(file, JSON.stringify(pkg, null, 2) + "\n");
  ' "$1" "$2"
}

# Reads the "name" field of a package.json.
package_name() {
  node -e '
    const fs = require("fs");
    const pkg = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    process.stdout.write(pkg.name || "");
  ' "$1"
}

count=0
for dir in "${NPM_DIR}"/*/; do
  [ -f "${dir}package.json" ] || continue
  pkg="$(basename "${dir}")"
  name="$(package_name "${dir}package.json")"
  # Platform packages are @localstack/lstk_<goos>_<goarch>; the wrapper is a
  # bare @localstack/lstk and carries no binary, so it is skipped here.
  case "${name}" in
    */lstk_*) goplatform="${name##*/lstk_}" ;;
    *) continue ;;
  esac
  src="${BUNDLED_DIR}/${goplatform}"
  [ -d "${src}" ] || die "no staged bundle for ${name} (${pkg}) at ${src}"

  added=""
  for file in "${src}"/bundled-extensions*; do
    [ -e "${file}" ] || die "no bundled-extensions binary in ${src} for ${pkg}"
    cp -p "${file}" "${dir}"
    added="${added} $(basename "${file}")"
  done
  cp "${TOML}" "${dir}"
  added="${added} lstk-extensions.toml"
  for doc in ${STAGED_BUNDLE_DOCUMENTS}; do
    cp "${BUNDLED_DIR}/${doc}" "${dir}"
    added="${added} ${doc}"
  done

  # shellcheck disable=SC2086 # deliberate word splitting of the collected names
  register_files "${dir}/package.json" ${added}
  set_license "${dir}/package.json" "${PLATFORM_LICENSE}"
  echo "${pkg}: added${added}; license: ${PLATFORM_LICENSE}"
  count=$((count + 1))
done

[ "${count}" -gt 0 ] || die "no platform packages found under ${NPM_DIR} (expected @localstack/lstk_<goos>_<goarch> packages)"
echo "Bundled extensions added to ${count} platform package(s)."
