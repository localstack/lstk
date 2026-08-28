#!/usr/bin/env bash
# Tests for scripts/add-bundled-to-npm.sh — the release step that copies the
# bundled extensions into each npm PLATFORM package and registers them in that
# package's `files` allowlist. Fixtures mirror goreleaser-npm-publisher's
# dist/npm layout: platform dirs named lstk-<node-os>-<node-cpu> with a
# package.json carrying "files": [], plus the lstk wrapper dir.
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/tests/lib.sh
. "${SUITE_DIR}/lib.sh"

ADD="${SUITE_DIR}/../add-bundled-to-npm.sh"
NPM_PLATFORMS="darwin-arm64 darwin-x64 linux-arm64 linux-x64 win32-arm64 win32-x64"
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

  for platform in ${NPM_PLATFORMS}; do
    mkdir -p "${NPM}/lstk-${platform}"
    local bin="lstk"
    case "${platform}" in win32-*) bin="lstk.exe" ;; esac
    echo "lstk binary" > "${NPM}/lstk-${platform}/${bin}"
    printf '{\n  "name": "@localstack/lstk-%s",\n  "version": "0.1.0",\n  "bin": {\n    "lstk": "%s"\n  },\n  "files": []\n}\n' \
      "${platform}" "${bin}" > "${NPM}/lstk-${platform}/package.json"
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
assert_file_exists "${NPM}/lstk-darwin-arm64/bundled-extensions"
assert_file_contains "${NPM}/lstk-darwin-arm64/bundled-extensions" "darwin_arm64"
assert_file_exists "${NPM}/lstk-linux-x64/bundled-extensions"
assert_file_contains "${NPM}/lstk-linux-x64/bundled-extensions" "linux_amd64"
assert_file_exists "${NPM}/lstk-win32-x64/bundled-extensions.exe"
assert_file_contains "${NPM}/lstk-win32-x64/bundled-extensions.exe" "windows_amd64"
assert_file_exists "${NPM}/lstk-win32-arm64/bundled-extensions.exe"
assert_file_contains "${NPM}/lstk-win32-arm64/bundled-extensions.exe" "windows_arm64"
assert_file_exists "${NPM}/lstk-darwin-x64/lstk-extensions.toml"

begin_test "the copied binary keeps its executable bit"
setup_workspace
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_ok
assert_executable "${NPM}/lstk-linux-arm64/bundled-extensions"

begin_test "registers the files in each platform package's files allowlist"
setup_workspace
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_ok
LAST_OUTPUT="$(files_field "${NPM}/lstk-darwin-arm64/package.json")"
assert_output_contains "bundled-extensions"
assert_output_contains "lstk-extensions.toml"
LAST_OUTPUT="$(files_field "${NPM}/lstk-win32-x64/package.json")"
assert_output_contains "bundled-extensions.exe"
assert_output_contains "lstk-extensions.toml"

begin_test "npm would actually pack the registered files"
setup_workspace
run_script "${ADD}" "${NPM}" "${BUNDLED}"
assert_ok
run_script npm pack --dry-run "${NPM}/lstk-darwin-arm64"
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
assert_output_contains "lstk-win32-arm64"
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
count="$(files_field "${NPM}/lstk-linux-x64/package.json" | tr ' ' '\n' | grep -c '^bundled-extensions$' || true)"
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
