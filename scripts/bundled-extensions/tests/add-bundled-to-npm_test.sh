#!/usr/bin/env bash
# Tests for scripts/add-bundled-to-npm.sh — the release step that copies the
# bundled extensions into each npm PLATFORM package and registers them in that
# package's `files` allowlist. Fixtures mirror goreleaser-npm-publisher's
# dist/npm layout: platform dirs named lstk-<node-os>-<node-cpu> with a
# package.json carrying "files": [], plus the lstk wrapper dir.
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/bundled-extensions/tests/lib.sh
. "${SUITE_DIR}/lib.sh"

ADD="${SUITE_DIR}/../add-bundled-to-npm.sh"
# The publisher slugifies its output directory names (lstk-darwin-arm-64-v-8-0)
# while the package.json inside carries the authoritative Go-style name
# (@localstack/lstk_darwin_arm64). Fixtures reproduce both, because parsing the
# directory name is exactly the bug these tests exist to prevent.
NPM_DIRS="lstk-darwin-arm-64-v-8-0 lstk-darwin-amd-64-v-1 lstk-linux-arm-64-v-8-0 lstk-linux-amd-64-v-1 lstk-windows-arm-64-v-8-0 lstk-windows-amd-64-v-1"
dir_to_goplatform() {
  case "$1" in
    lstk-darwin-arm-64*) echo darwin_arm64 ;;
    lstk-darwin-amd-64*) echo darwin_amd64 ;;
    lstk-linux-arm-64*) echo linux_arm64 ;;
    lstk-linux-amd-64*) echo linux_amd64 ;;
    lstk-windows-arm-64*) echo windows_arm64 ;;
    lstk-windows-amd-64*) echo windows_amd64 ;;
  esac
}
GO_PLATFORMS="darwin_arm64 darwin_amd64 linux_arm64 linux_amd64 windows_arm64 windows_amd64"

# Fresh workspace with a full staged bundle and a full dist/npm tree.
# Sets WORK, BUNDLED, NPM.
setup_workspace() {
  WORK="$(mktemp -d)"
  BUNDLED="${WORK}/bundled"
  NPM="${WORK}/dist/npm"
  local platform
  for platform in ${GO_PLATFORMS}; do
    mkdir -p "${BUNDLED}/${platform}"
    case "${platform}" in
      windows_*) echo "bin ${platform}" > "${BUNDLED}/${platform}/bundled-extensions.exe" ;;
      *) echo "bin ${platform}" > "${BUNDLED}/${platform}/bundled-extensions" ;;
    esac
    chmod 0755 "${BUNDLED}/${platform}"/bundled-extensions*
  done
  echo 'doctor = "Check the local setup"' > "${BUNDLED}/lstk-extensions.toml"

  local d goplat bin
  for d in ${NPM_DIRS}; do
    goplat="$(dir_to_goplatform "${d}")"
    mkdir -p "${NPM}/${d}"
    bin="lstk"
    case "${goplat}" in windows_*) bin="lstk.exe" ;; esac
    echo "lstk binary" > "${NPM}/${d}/${bin}"
    printf '{\n  "name": "@localstack/lstk_%s",\n  "version": "0.1.0",\n  "bin": {\n    "lstk_%s": "%s"\n  },\n  "files": []\n}\n' \
      "${goplat}" "${goplat}" "${bin}" > "${NPM}/${d}/package.json"
  done
  mkdir -p "${NPM}/lstk"
  echo "launcher" > "${NPM}/lstk/index.js"
  printf '{\n  "name": "@localstack/lstk",\n  "version": "0.1.0",\n  "bin": {\n    "lstk": "index.js"\n  },\n  "files": []\n}\n' > "${NPM}/lstk/package.json"
}

files_field() {
  node -e 'const p=JSON.parse(require("fs").readFileSync(process.argv[1]));console.log((p.files||[]).join(" "))' "$1"
}

echo "== add-bundled-to-npm.sh =="

begin_test "copies the matching platform binary and the toml into every platform package"
setup_workspace
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_ok
assert_file_exists "${NPM}/lstk-darwin-arm-64-v-8-0/bundled-extensions"
assert_file_contains "${NPM}/lstk-darwin-arm-64-v-8-0/bundled-extensions" "darwin_arm64"
assert_file_exists "${NPM}/lstk-linux-amd-64-v-1/bundled-extensions"
assert_file_contains "${NPM}/lstk-linux-amd-64-v-1/bundled-extensions" "linux_amd64"
assert_file_exists "${NPM}/lstk-windows-amd-64-v-1/bundled-extensions.exe"
assert_file_contains "${NPM}/lstk-windows-amd-64-v-1/bundled-extensions.exe" "windows_amd64"
assert_file_exists "${NPM}/lstk-windows-arm-64-v-8-0/bundled-extensions.exe"
assert_file_contains "${NPM}/lstk-windows-arm-64-v-8-0/bundled-extensions.exe" "windows_arm64"
assert_file_exists "${NPM}/lstk-darwin-amd-64-v-1/lstk-extensions.toml"

begin_test "the copied binary keeps its executable bit"
setup_workspace
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_ok
assert_executable "${NPM}/lstk-linux-arm-64-v-8-0/bundled-extensions"

begin_test "registers the files in each platform package's files allowlist"
setup_workspace
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_ok
LAST_OUTPUT="$(files_field "${NPM}/lstk-darwin-arm-64-v-8-0/package.json")"
assert_output_contains "bundled-extensions"
assert_output_contains "lstk-extensions.toml"
LAST_OUTPUT="$(files_field "${NPM}/lstk-windows-amd-64-v-1/package.json")"
assert_output_contains "bundled-extensions.exe"
assert_output_contains "lstk-extensions.toml"

begin_test "npm would actually pack the registered files"
setup_workspace
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_ok
run_script npm pack --dry-run "${NPM}/lstk-darwin-arm-64-v-8-0"
assert_ok
assert_output_contains "bundled-extensions"
assert_output_contains "lstk-extensions.toml"
assert_output_contains " lstk"

begin_test "leaves the wrapper package untouched"
setup_workspace
before="$(cat "${NPM}/lstk/package.json")"
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_ok
assert_file_absent "${NPM}/lstk/bundled-extensions"
assert_file_absent "${NPM}/lstk/lstk-extensions.toml"
[ "$(cat "${NPM}/lstk/package.json")" = "${before}" ] || fail "wrapper package.json was modified"
assert_file_contains "${NPM}/lstk/index.js" "launcher"

begin_test "fails naming the package when its platform has no staged bundle"
setup_workspace
rm -rf "${BUNDLED}/windows_arm64"
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_fails
assert_output_contains "lstk-windows-arm-64-v-8-0"
assert_output_contains "windows_arm64"

begin_test "fails when the toml is missing"
setup_workspace
rm "${BUNDLED}/lstk-extensions.toml"
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_fails
assert_output_contains "lstk-extensions.toml"

begin_test "is idempotent: a second run does not duplicate files entries"
setup_workspace
run_script "${ADD}" "${NPM}" "${BUNDLED}"
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_ok
count="$(files_field "${NPM}/lstk-linux-amd-64-v-1/package.json" | tr ' ' '\n' | grep -c '^bundled-extensions$' || true)"
[ "${count}" -eq 1 ] || fail "expected one bundled-extensions entry, got ${count}"

begin_test "fails when there are no platform packages at all"
setup_workspace
rm -rf "${NPM}"/lstk-*
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_fails
assert_output_contains "no platform packages"

begin_test "missing arguments print usage and fail"
run_script "${ADD}"
assert_fails
assert_output_contains "sage"

finish_suite
