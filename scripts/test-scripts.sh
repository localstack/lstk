#!/usr/bin/env bash
# Runs the bash test suites for the release helper scripts under scripts/.
# These scripts only ever run on the Linux release runner, so a bash suite is
# the faithful test here; lstk's own behavior is covered by the Go suites.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

status=0
for suite in "${SCRIPT_DIR}"/tests/*_test.sh; do
  [ -e "${suite}" ] || continue
  bash "${suite}" || status=1
  echo
done
exit "${status}"
