#!/bin/sh
# Stage of .spiral/check.sh: unit tests under cmd/ and internal/ (CI: Unit
# Tests), and the npm launcher's tests (CI: Launcher Tests).
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

step "unit tests (make test)"
make test

step "launcher tests (node --test npm/*.test.js)"
node --test npm/*.test.js
