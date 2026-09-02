#!/usr/bin/env bash
# Tests for scripts/check-descriptions.sh — the release gate that keeps the
# descriptions file and the multi-call bundled binary in agreement. Fixtures are
# built in temp dirs mirroring the staging layout the fetch script produces: a
# platform dir holding the binary, and the toml one level up. The binary is a
# stub script answering `list`, which is where the gate gets the bundle's own
# command list.
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/tests/lib.sh
. "${SUITE_DIR}/lib.sh"

CHECK="${SUITE_DIR}/../check-descriptions.sh"

# Fresh staging tree. Sets STAGE (the bundled/ root) and PLATFORM_DIR.
setup_stage() {
  STAGE="$(mktemp -d)"
  PLATFORM_DIR="${STAGE}/linux_amd64"
  mkdir -p "${PLATFORM_DIR}"
}

# A plain non-bundle file in the platform dir, e.g. a standalone extension.
write_binary() {
  echo "fake" > "${PLATFORM_DIR}/${1:-bundled-extensions}"
  chmod 0755 "${PLATFORM_DIR}/${1:-bundled-extensions}"
}

write_toml() {
  printf '%s' "$1" > "${STAGE}/lstk-extensions.toml"
}

# The bundle binary, answering `list` with the given command names — the
# bundle's own statement of what it provides.
write_bundle() {
  {
    echo '#!/bin/sh'
    echo 'if [ "$1" = "list" ]; then'
    for name in "$@"; do
      echo "  echo '${name}'"
    done
    echo '  exit 0'
    echo 'fi'
    echo 'echo "stub bundle"'
  } > "${PLATFORM_DIR}/bundled-extensions"
  chmod 0755 "${PLATFORM_DIR}/bundled-extensions"
}

# A bundle binary whose `list` fails or prints something unusable.
write_broken_bundle() {
  printf '#!/bin/sh\n%s\n' "$1" > "${PLATFORM_DIR}/bundled-extensions"
  chmod 0755 "${PLATFORM_DIR}/bundled-extensions"
}

echo "== check-descriptions.sh =="

begin_test "binary, descriptions and command list agree: passes and lists the commands"
setup_stage
write_bundle doctor deploy
write_toml 'doctor = "Check the local setup"
deploy = "Deploy to LocalStack"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok
assert_output_contains "doctor"
assert_output_contains "deploy"
assert_output_lacks "Warning"

begin_test "a described command the bundle does not provide fails, naming it"
setup_stage
write_bundle doctor
write_toml 'doctor = "Check the local setup"
deploy = "Deploy to LocalStack"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "deploy"
assert_output_contains "does not provide"

begin_test "a bundle command that is not described warns but passes"
setup_stage
write_bundle doctor deploy
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok
assert_output_contains "Warning"
assert_output_contains "deploy"

begin_test "a bundle whose list command fails is fatal, showing its own output"
setup_stage
write_broken_bundle 'echo "boom" >&2; exit 3'
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "boom"
assert_output_contains "cannot read the bundle"

begin_test "a bundle that lists nothing fails"
setup_stage
write_bundle
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "printed no commands"

begin_test "a bundle listing something that is not a command name fails, naming it"
setup_stage
write_broken_bundle 'echo "doctor - Check the local setup"'
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "doctor - Check the local setup"
assert_output_contains "one bare command name per line"

begin_test "blank lines and surrounding whitespace in the list are tolerated"
setup_stage
write_broken_bundle 'printf "doctor  \n\n deploy\n"'
write_toml 'doctor = "Check the local setup"
deploy = "Deploy to LocalStack"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok
assert_output_lacks "Warning"

begin_test "described commands with no bundled binary fail, naming them"
setup_stage
write_toml 'doctor = "Check the local setup"
deploy = "Deploy to LocalStack"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "bundled-extensions"
assert_output_contains "doctor"
assert_output_contains "deploy"

begin_test "a bundled binary with no descriptions file fails"
setup_stage
write_bundle doctor
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "lstk-extensions.toml"

begin_test "a bundled binary with an empty descriptions file fails"
setup_stage
write_bundle doctor
write_toml '# nothing described yet
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "no commands"

begin_test "nothing bundled at all passes"
setup_stage
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok

begin_test "an empty descriptions file with no binary passes"
setup_stage
write_toml ''
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok

begin_test "the Windows binary name is recognised as the bundle"
setup_stage
write_binary bundled-extensions.exe
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
# Recognised, then run — which is as far as this gets on a host that cannot
# execute it. The release checks a platform directory it can run.
assert_fails
assert_output_contains "bundled-extensions.exe"
assert_output_contains "a platform directory this machine can execute"

begin_test "a non-executable binary fails"
setup_stage
write_bundle doctor
chmod 0644 "${PLATFORM_DIR}/bundled-extensions"
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "executable"

begin_test "only the left-hand names are read, never the values"
setup_stage
write_bundle doctor
# A hostile description: quotes, an equals sign, a fake key on the same line.
write_toml 'doctor = "a = b \"quoted\" evil = \"x\""
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok
assert_output_contains "doctor"
assert_output_lacks "evil"

begin_test "an invalid command name fails"
setup_stage
write_bundle doctor
write_toml 'doc tor = "spaces are not a command"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "doc tor"

begin_test "a bundle staged on its own passes with no warnings"
setup_stage
write_bundle doctor
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok
assert_output_lacks "Warning"

begin_test "a stray standalone lstk-<name> binary warns but passes"
setup_stage
write_binary lstk-legacy
write_bundle doctor
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok
assert_output_contains "Warning"
assert_output_contains "lstk-legacy"

begin_test "an explicit toml path overrides the default location"
setup_stage
write_bundle doctor
OTHER_DIR="$(mktemp -d)"
printf 'doctor = "x"\n' > "${OTHER_DIR}/other.toml"
run_script "${CHECK}" "${PLATFORM_DIR}" "${OTHER_DIR}/other.toml"
assert_ok
assert_output_contains "doctor"

begin_test "a missing platform directory fails and names it"
run_script "${CHECK}" "/nonexistent/linux_amd64"
assert_fails
assert_output_contains "/nonexistent/linux_amd64"

begin_test "no argument prints usage and fails"
run_script "${CHECK}"
assert_fails
assert_output_contains "sage"

finish_suite
