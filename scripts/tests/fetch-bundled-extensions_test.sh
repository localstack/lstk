#!/usr/bin/env bash
# Tests for scripts/fetch-bundled-extensions.sh.
#
# The private extensions repository is mocked with a fake `gh` on PATH, so the
# suite never needs the real repo or a credential. Fixtures reproduce what that
# repo actually publishes: one archive per platform holding the multi-call
# binary, the descriptions file and lstk-<name> alias entries, plus a
# checksums.txt over the archives. Every assertion is about what the script
# leaves on disk and what it prints — the two things the release consumes.
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/tests/lib.sh
. "${SUITE_DIR}/lib.sh"

FETCH="${SUITE_DIR}/../fetch-bundled-extensions.sh"
PLATFORMS="linux_amd64 linux_arm64 darwin_amd64 darwin_arm64 windows_amd64 windows_arm64"

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# Writes a stand-in bundle binary that answers `list` with COMMANDS (default
# "doctor"), the way the real one does.
write_fake_bundle() {
  {
    echo '#!/bin/sh'
    echo 'if [ "$1" = "list" ]; then'
    for name in ${COMMANDS-doctor}; do
      echo "  echo '${name}'"
    done
    echo '  exit 0'
    echo 'fi'
    echo "echo 'fake bundle binary for $2'"
  } > "$1"
  chmod 0755 "$1"
}

# Builds a fixture release for the given tag: per platform, a tar.gz (zip for
# Windows) containing bundled-extensions[.exe] and lstk-extensions.toml, plus a
# checksums.txt over the archives. ALIASES adds lstk-<name> entries the way the
# private repo used to emit them (a symlink in the tarballs, a copy in the
# zips), so the tests can prove they are ignored. TOML_BODY overrides the
# descriptions file for every platform.
make_release_assets() {
  local dir="$1" tag="${2:-v1.4.0}"
  local toml_body="${TOML_BODY-doctor = \"Fake doctor description\"
}"
  mkdir -p "${dir}"
  local platform work
  for platform in ${PLATFORMS}; do
    work="$(mktemp -d)"
    printf '%s' "${toml_body}" > "${work}/lstk-extensions.toml"
    case "${platform}" in
      windows_*)
        write_fake_bundle "${work}/bundled-extensions.exe" "${platform}"
        for alias in ${ALIASES-}; do cp "${work}/bundled-extensions.exe" "${work}/lstk-${alias}.exe"; done
        ( cd "${work}" && zip -q -r "${dir}/bundled-extensions_${tag}_${platform}.zip" . )
        ;;
      *)
        write_fake_bundle "${work}/bundled-extensions" "${platform}"
        for alias in ${ALIASES-}; do ( cd "${work}" && ln -s bundled-extensions "lstk-${alias}" ); done
        ( cd "${work}" && tar czf "${dir}/bundled-extensions_${tag}_${platform}.tar.gz" . )
        ;;
    esac
    rm -rf "${work}"
  done
  ( cd "${dir}" && for asset in *; do
      [ "${asset}" = "checksums.txt" ] && continue
      echo "$(sha256_of "${asset}")  ${asset}"
    done > checksums.txt )
}

# Rewrites checksums.txt after a fixture was modified (used by tests that want
# a valid manifest for a deliberately altered set of assets).
refresh_manifest() {
  ( cd "$1" && for asset in *; do
      [ "${asset}" = "checksums.txt" ] && continue
      echo "$(sha256_of "${asset}")  ${asset}"
    done > checksums.txt )
}

# A `gh` stand-in covering the two subcommands the fetch script uses.
install_fake_gh() {
  local bindir="$1" assets="$2" latest_tag="$3"
  mkdir -p "${bindir}"
  cat > "${bindir}/gh" <<FAKE
#!/usr/bin/env bash
set -euo pipefail
echo "gh \$*" >> "${bindir}/gh.log"
if [ "\$1" = "release" ] && [ "\$2" = "view" ]; then
  echo "${latest_tag}"
  exit 0
fi
if [ "\$1" = "release" ] && [ "\$2" = "download" ]; then
  dest=""
  while [ \$# -gt 0 ]; do
    if [ "\$1" = "--dir" ]; then dest="\$2"; fi
    shift
  done
  mkdir -p "\$dest"
  cp "${assets}"/* "\$dest"/
  exit 0
fi
echo "fake gh: unsupported invocation: \$*" >&2
exit 1
FAKE
  chmod +x "${bindir}/gh"
}

# Fresh workspace: a bundled dir with a version file, a fake gh on PATH, and a
# fixture release. Sets BUNDLED, BINDIR and ASSETS for the calling test.
setup_workspace() {
  local version_value="${1:-latest}" latest_tag="${2:-v1.4.0}"
  WORK="$(mktemp -d)"
  BUNDLED="${WORK}/bundled"
  BINDIR="${WORK}/bin"
  ASSETS="${WORK}/assets"
  mkdir -p "${BUNDLED}"
  echo "${version_value}" > "${BUNDLED}/extensions.version"
  make_release_assets "${ASSETS}" "${latest_tag}"
  install_fake_gh "${BINDIR}" "${ASSETS}" "${latest_tag}"
  export LSTK_BUNDLED_DIR="${BUNDLED}"
  export LSTK_EXTENSIONS_READ_TOKEN="fake-token"
  export LSTK_EXTENSIONS_REPO="localstack/fake-extensions"
  export PATH="${BINDIR}:${ORIGINAL_PATH}"
  unset LSTK_EXTENSIONS_TAG || true
}

ORIGINAL_PATH="${PATH}"

echo "== fetch-bundled-extensions.sh =="

begin_test "fails without a token, naming the variable and the --stub alternative"
setup_workspace
unset LSTK_EXTENSIONS_READ_TOKEN
run_script "${FETCH}"
assert_fails
assert_output_contains "LSTK_EXTENSIONS_READ_TOKEN"
assert_output_contains "--stub"

begin_test "--stub stages the real layout for every platform without a token or gh"
setup_workspace
unset LSTK_EXTENSIONS_READ_TOKEN
export PATH="${ORIGINAL_PATH}"
run_script "${FETCH}" --stub
assert_ok
for platform in ${PLATFORMS}; do
  suffix=""
  case "${platform}" in windows_*) suffix=".exe" ;; esac
  assert_executable "${BUNDLED}/${platform}/bundled-extensions${suffix}"
done
assert_file_exists "${BUNDLED}/lstk-extensions.toml"
# The stub toml must describe at least one command, or the descriptions gate
# (and lstk itself) would reject the pairing.
assert_file_contains "${BUNDLED}/lstk-extensions.toml" "doctor"

begin_test "--stub prints an unmissable never-release banner"
setup_workspace
run_script "${FETCH}" --stub
assert_ok
assert_output_contains "STUB"
assert_output_contains "must never be released"

begin_test "--stub honours an explicit binary list"
setup_workspace
LSTK_BUNDLED_STUB_BINARIES="lstk-doctor" run_script "${FETCH}" --stub
assert_ok
assert_executable "${BUNDLED}/linux_amd64/lstk-doctor"
assert_file_absent "${BUNDLED}/linux_amd64/bundled-extensions"
assert_file_contains "${BUNDLED}/lstk-extensions.toml" "doctor"

begin_test "--stub output passes the descriptions gate"
setup_workspace
run_script "${FETCH}" --stub
run_script "${SUITE_DIR}/../check-descriptions.sh" "${BUNDLED}/linux_amd64"
assert_ok

begin_test "resolves 'latest' to a concrete tag and prints it"
setup_workspace latest v2.1.3
run_script "${FETCH}"
assert_ok
assert_output_contains "v2.1.3"

begin_test "downloads the resolved tag rather than 'latest'"
setup_workspace latest v2.1.3
run_script "${FETCH}"
assert_ok
assert_file_contains "${BINDIR}/gh.log" "release download v2.1.3"
run_script grep -c "release download latest" "${BINDIR}/gh.log"
assert_fails

begin_test "an explicit tag in the version file is used without resolving"
setup_workspace v0.9.1 v0.9.1
run_script "${FETCH}"
assert_ok
assert_file_contains "${BINDIR}/gh.log" "release download v0.9.1"
run_script grep -c "release view" "${BINDIR}/gh.log"
assert_fails

begin_test "--tag overrides the version file"
setup_workspace latest v0.5.0
run_script "${FETCH}" --tag v0.5.0
assert_ok
assert_file_contains "${BINDIR}/gh.log" "release download v0.5.0"
run_script grep -c "release view" "${BINDIR}/gh.log"
assert_fails

begin_test "LSTK_EXTENSIONS_TAG overrides the version file"
setup_workspace latest v0.4.2
LSTK_EXTENSIONS_TAG=v0.4.2 run_script "${FETCH}"
assert_ok
assert_file_contains "${BINDIR}/gh.log" "release download v0.4.2"
run_script grep -c "release view" "${BINDIR}/gh.log"
assert_fails

begin_test "unpacks each archive into its platform dir with the executable bit, toml at the root"
setup_workspace
run_script "${FETCH}"
assert_ok
for platform in ${PLATFORMS}; do
  suffix=""
  case "${platform}" in windows_*) suffix=".exe" ;; esac
  assert_executable "${BUNDLED}/${platform}/bundled-extensions${suffix}"
  assert_file_contains "${BUNDLED}/${platform}/bundled-extensions${suffix}" "${platform}"
done
assert_file_exists "${BUNDLED}/lstk-extensions.toml"
assert_file_contains "${BUNDLED}/lstk-extensions.toml" "doctor"
assert_file_absent "${BUNDLED}/linux_amd64/lstk-extensions.toml"
assert_file_absent "${BUNDLED}/linux_amd64/checksums.txt"

begin_test "the archive's alias entries are never staged, on any platform"
setup_workspace
run_script "${FETCH}"
assert_ok
for platform in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
  assert_file_absent "${BUNDLED}/${platform}/lstk-doctor"
  assert_executable "${BUNDLED}/${platform}/bundled-extensions"
done
assert_file_absent "${BUNDLED}/windows_amd64/lstk-doctor.exe"
assert_file_absent "${BUNDLED}/windows_arm64/lstk-doctor.exe"
assert_executable "${BUNDLED}/windows_amd64/bundled-extensions.exe"

begin_test "a multi-command bundle still stages only the binary and the toml"
setup_workspace
COMMANDS="doctor deploy" TOML_BODY='doctor = "x"
deploy = "y"
' make_release_assets "${ASSETS}"
run_script "${FETCH}"
assert_ok
assert_executable "${BUNDLED}/linux_amd64/bundled-extensions"
assert_file_exists "${BUNDLED}/lstk-extensions.toml"
assert_file_absent "${BUNDLED}/linux_amd64/lstk-doctor"
assert_file_absent "${BUNDLED}/linux_amd64/lstk-deploy"

begin_test "the staged tree passes the descriptions gate"
setup_workspace
run_script "${FETCH}"
run_script "${SUITE_DIR}/../check-descriptions.sh" "${BUNDLED}/linux_amd64"
assert_ok

begin_test "a checksum mismatch aborts the fetch and names the asset"
setup_workspace
echo "tampered" >> "${ASSETS}/bundled-extensions_v1.4.0_linux_amd64.tar.gz"
run_script "${FETCH}"
assert_fails
assert_output_contains "bundled-extensions_v1.4.0_linux_amd64.tar.gz"
assert_file_absent "${BUNDLED}/linux_amd64/bundled-extensions"

begin_test "a missing checksum manifest aborts the fetch"
setup_workspace
rm "${ASSETS}/checksums.txt"
run_script "${FETCH}"
assert_fails
assert_output_contains "checksums.txt"

begin_test "an asset absent from the manifest aborts the fetch"
setup_workspace
cp "${ASSETS}/bundled-extensions_v1.4.0_linux_amd64.tar.gz" "${ASSETS}/bundled-extensions_v1.4.0_linux_386.tar.gz"
run_script "${FETCH}"
assert_fails
assert_output_contains "bundled-extensions_v1.4.0_linux_386.tar.gz"

begin_test "an archive without the bundled binary aborts the fetch"
setup_workspace
work="$(mktemp -d)"
printf 'doctor = "x"\n' > "${work}/lstk-extensions.toml"
( cd "${work}" && tar czf "${ASSETS}/bundled-extensions_v1.4.0_linux_amd64.tar.gz" . )
refresh_manifest "${ASSETS}"
run_script "${FETCH}"
assert_fails
assert_output_contains "bundled-extensions_v1.4.0_linux_amd64.tar.gz"
assert_output_contains "contains no bundled-extensions"

begin_test "an archive without the descriptions file aborts the fetch"
setup_workspace
work="$(mktemp -d)"
echo "bin" > "${work}/bundled-extensions"
( cd "${work}" && tar czf "${ASSETS}/bundled-extensions_v1.4.0_darwin_arm64.tar.gz" . )
refresh_manifest "${ASSETS}"
run_script "${FETCH}"
assert_fails
assert_output_contains "contains no lstk-extensions.toml"

begin_test "descriptions differing between platforms abort the fetch"
setup_workspace
work="$(mktemp -d)"
echo "bin" > "${work}/bundled-extensions"
printf 'deploy = "a different command list"\n' > "${work}/lstk-extensions.toml"
( cd "${work}" && tar czf "${ASSETS}/bundled-extensions_v1.4.0_linux_arm64.tar.gz" . )
refresh_manifest "${ASSETS}"
run_script "${FETCH}"
assert_fails
assert_output_contains "differs"

begin_test "a platform with no archive fails and names the platform"
setup_workspace
rm "${ASSETS}/bundled-extensions_v1.4.0_darwin_arm64.tar.gz"
refresh_manifest "${ASSETS}"
run_script "${FETCH}"
assert_fails
assert_output_contains "darwin_arm64"
assert_output_contains "bundled-extensions"

begin_test "UNSUPPORTED_PLATFORMS exempts a platform from the coverage check"
setup_workspace
rm "${ASSETS}/bundled-extensions_v1.4.0_darwin_arm64.tar.gz"
refresh_manifest "${ASSETS}"
LSTK_UNSUPPORTED_PLATFORMS="darwin_arm64" run_script "${FETCH}"
assert_ok
assert_file_absent "${BUNDLED}/darwin_arm64/bundled-extensions"

begin_test "a non-archive extra asset is ignored with a note"
setup_workspace
echo "notes" > "${ASSETS}/RELEASE_NOTES.md"
refresh_manifest "${ASSETS}"
run_script "${FETCH}"
assert_ok
assert_output_contains "RELEASE_NOTES.md"
assert_file_absent "${BUNDLED}/RELEASE_NOTES.md"

begin_test "a missing version file fails and names it"
setup_workspace
rm "${BUNDLED}/extensions.version"
run_script "${FETCH}"
assert_fails
assert_output_contains "extensions.version"

begin_test "the version file ignores comments and blank lines"
setup_workspace latest v0.7.7
printf '# which bundle to ship\n\nv0.7.7\n' > "${BUNDLED}/extensions.version"
run_script "${FETCH}"
assert_ok
assert_file_contains "${BINDIR}/gh.log" "release download v0.7.7"

begin_test "a stale staging tree is replaced rather than merged into"
setup_workspace
mkdir -p "${BUNDLED}/linux_amd64"
echo stale > "${BUNDLED}/linux_amd64/lstk-removed"
run_script "${FETCH}"
assert_ok
assert_file_absent "${BUNDLED}/linux_amd64/lstk-removed"
assert_file_exists "${BUNDLED}/extensions.version"

begin_test "an archive still carrying lstk-<name> entries stages neither them nor a command list"
setup_workspace
ALIASES="doctor deploy" make_release_assets "${ASSETS}"
run_script "${FETCH}"
assert_ok
assert_file_absent "${BUNDLED}/linux_amd64/lstk-doctor"
assert_file_absent "${BUNDLED}/linux_amd64/lstk-deploy"
assert_file_absent "${BUNDLED}/bundle-commands.txt"

begin_test "an archive with no lstk-<name> entries is fine; the binary declares its own commands"
setup_workspace
work="$(mktemp -d)"
write_fake_bundle "${work}/bundled-extensions" darwin_amd64
printf 'doctor = "Fake doctor description"\n' > "${work}/lstk-extensions.toml"
( cd "${work}" && tar czf "${ASSETS}/bundled-extensions_v1.4.0_darwin_amd64.tar.gz" . )
refresh_manifest "${ASSETS}"
run_script "${FETCH}"
assert_ok
assert_executable "${BUNDLED}/darwin_amd64/bundled-extensions"

begin_test "--stub produces a bundle that answers list and passes the gate"
setup_workspace
run_script "${FETCH}" --stub
assert_ok
run_script "${BUNDLED}/linux_amd64/bundled-extensions" list
assert_ok
assert_output_contains "doctor"
run_script "${SUITE_DIR}/../check-descriptions.sh" "${BUNDLED}/linux_amd64"
assert_ok

finish_suite
