#!/bin/sh
# Stage of .spiral/check.sh: golangci-lint on both modules, at the version
# .tool-versions pins (CI: Lint).
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

step "golangci-lint (make lint)"
make lint
