#!/bin/sh
# Stage of .spiral/check.sh: gitleaks over the branch's commits, the scan
# developers get from the pre-commit hook (.pre-commit-config.yaml). CI does
# not run it, so without this stage nothing scans Spiral's commits.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

step "gitleaks on commits $base..HEAD"
gitleaks git --log-opts="$base..HEAD" --redact --no-banner .
