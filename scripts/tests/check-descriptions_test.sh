#!/usr/bin/env bash
# Tests for scripts/check-descriptions.sh — the release gate that keeps the
# descriptions file and the multi-call bundled binary in agreement. Fixtures are
# built in temp dirs mirroring the staging layout the fetch script produces:
# a platform dir holding the binary, and the toml plus the bundle's own command
# list (bundle-commands.txt) one level up.
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

write_binary() {
  echo "fake" > "${PLATFORM_DIR}/${1:-bundled-extensions}"
  chmod 0755 "${PLATFORM_DIR}/${1:-bundled-extensions}"
}

write_toml() {
  printf '%s' "$1" > "${STAGE}/lstk-extensions.toml"
}

# The command list the fetch script records from the bundle's own lstk-<name>
# alias entries: what the binary actually answers to.
write_commands() {
  : > "${STAGE}/bundle-commands.txt"
  for name in "$@"; do
    echo "${name}" >> "${STAGE}/bundle-commands.txt"
  done
}

echo "== check-descriptions.sh =="

begin_test "binary, descriptions and command list agree: passes and lists the commands"
setup_stage
write_binary
write_commands doctor deploy
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
write_binary
write_commands doctor
write_toml 'doctor = "Check the local setup"
deploy = "Deploy to LocalStack"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "deploy"
assert_output_contains "does not provide"

begin_test "a bundle command that is not described warns but passes"
setup_stage
write_binary
write_commands doctor deploy
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok
assert_output_contains "Warning"
assert_output_contains "deploy"

begin_test "a bundled binary with no command list fails, naming the file"
setup_stage
write_binary
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "bundle-commands.txt"

begin_test "a bundled binary with an empty command list fails"
setup_stage
write_binary
write_commands
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "bundle-commands.txt"

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
write_binary
write_commands doctor
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "lstk-extensions.toml"

begin_test "a bundled binary with an empty descriptions file fails"
setup_stage
write_binary
write_commands doctor
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

begin_test "the Windows binary name is accepted"
setup_stage
write_binary bundled-extensions.exe
write_commands doctor
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok

begin_test "a non-executable binary fails"
setup_stage
echo "fake" > "${PLATFORM_DIR}/bundled-extensions"
chmod 0644 "${PLATFORM_DIR}/bundled-extensions"
write_commands doctor
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "executable"

begin_test "only the left-hand names are read, never the values"
setup_stage
write_binary
write_commands doctor
# A hostile description: quotes, an equals sign, a fake key on the same line.
write_toml 'doctor = "a = b \"quoted\" evil = \"x\""
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok
assert_output_contains "doctor"
assert_output_lacks "evil"

begin_test "an invalid command name fails"
setup_stage
write_binary
write_commands doctor
write_toml 'doc tor = "spaces are not a command"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_fails
assert_output_contains "doc tor"

begin_test "a stray standalone lstk-<name> binary warns but passes"
setup_stage
write_binary
write_binary lstk-legacy
write_commands doctor
write_toml 'doctor = "Check the local setup"
'
run_script "${CHECK}" "${PLATFORM_DIR}"
assert_ok
assert_output_contains "Warning"
assert_output_contains "lstk-legacy"

begin_test "explicit toml and command-list paths override the default locations"
setup_stage
write_binary
OTHER_DIR="$(mktemp -d)"
printf 'doctor = "x"\n' > "${OTHER_DIR}/other.toml"
printf 'doctor\n' > "${OTHER_DIR}/other-commands.txt"
run_script "${CHECK}" "${PLATFORM_DIR}" "${OTHER_DIR}/other.toml" "${OTHER_DIR}/other-commands.txt"
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
