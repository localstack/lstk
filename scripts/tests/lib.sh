#!/usr/bin/env bash
# Minimal assertion helpers for the release-script tests. Kept bash 3.2
# compatible (no associative arrays, namerefs or mapfile) so the suite runs on
# a stock macOS shell as well as on the Linux release runner.

TESTS_RUN=0
TESTS_FAILED=0
CURRENT_TEST=""
CURRENT_FAILED=0

# Captured by run_script for the assertions below.
LAST_STATUS=0
LAST_OUTPUT=""

fail() {
  # Counted once per test, however many assertions in it fail.
  if [ "${CURRENT_FAILED}" -eq 0 ]; then
    CURRENT_FAILED=1
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  echo "  FAIL: ${CURRENT_TEST}"
  echo "    $1"
  if [ -n "${LAST_OUTPUT}" ]; then
    echo "    --- captured output ---"
    echo "${LAST_OUTPUT}" | sed 's/^/    /'
    echo "    -----------------------"
  fi
}

begin_test() {
  CURRENT_TEST="$1"
  CURRENT_FAILED=0
  TESTS_RUN=$((TESTS_RUN + 1))
  LAST_STATUS=0
  LAST_OUTPUT=""
}

# Runs a command, capturing stdout+stderr and the exit status instead of
# aborting the suite. Every assertion below reads what this recorded.
run_script() {
  set +e
  LAST_OUTPUT="$("$@" 2>&1)"
  LAST_STATUS=$?
  set -e
}

assert_ok() {
  [ "${LAST_STATUS}" -eq 0 ] || fail "expected success, got exit status ${LAST_STATUS}"
}

assert_fails() {
  [ "${LAST_STATUS}" -ne 0 ] || fail "expected a non-zero exit status, got success"
}

assert_output_contains() {
  case "${LAST_OUTPUT}" in
    *"$1"*) ;;
    *) fail "expected output to contain: $1" ;;
  esac
}

assert_output_lacks() {
  case "${LAST_OUTPUT}" in
    *"$1"*) fail "expected output NOT to contain: $1" ;;
  esac
}

assert_file_exists() {
  [ -f "$1" ] || fail "expected file to exist: $1"
}

assert_file_absent() {
  [ ! -e "$1" ] || fail "expected file NOT to exist: $1"
}

assert_executable() {
  [ -x "$1" ] || fail "expected file to be executable: $1"
}

assert_file_contains() {
  if [ ! -f "$1" ]; then
    fail "expected file to exist: $1"
    return
  fi
  grep -q -- "$2" "$1" || fail "expected $1 to contain: $2"
}

finish_suite() {
  echo
  if [ "${TESTS_FAILED}" -gt 0 ]; then
    echo "${TESTS_FAILED}/${TESTS_RUN} test(s) failed in $(basename "$0")"
    exit 1
  fi
  echo "${TESTS_RUN}/${TESTS_RUN} test(s) passed in $(basename "$0")"
}
