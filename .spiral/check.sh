#!/bin/sh
# Spiral gate for lstk. Spiral runs it with /bin/sh from the checkout root after
# every implementer turn (spiral: internal/worker/implementer.go, runChecks): a
# non-zero exit goes back to the implementer as `checks-failed`, zero lets the
# reviewer run. It mirrors the pre-commit hooks and CI's merge-path jobs as far
# as a worker can run them.
#
# The stages live in .spiral/checks/, each runnable on its own from the repo root:
#   openspec.sh      only new openspec changes, validated with every task ticked;
#                    Go changes under cmd/ or internal/ need one.
#   gitleaks.sh      the pre-commit hook's secret scan, over the branch's commits.
#   lint.sh          golangci-lint on both modules (CI: Lint).
#   unit.sh          make test and the npm launcher tests (CI: Unit Tests,
#                    Launcher Tests).
#   integration.sh   the integration suite with Docker unreachable, so the
#                    Docker- and token-backed tests skip (CI: Integration Tests).
#
# Left to CI: those Docker tests, the macOS and Windows runs, govulncheck
# (vuln.go.dev is closed on workers), and the release checks (GoReleaser,
# release scripts, licences, the Homebrew cask, zizmor on workflows).
# shellcheck source=checks/lib.sh
. "$(dirname "$0")/checks/lib.sh"

sh .spiral/checks/openspec.sh
sh .spiral/checks/gitleaks.sh
sh .spiral/checks/lint.sh
sh .spiral/checks/unit.sh
sh .spiral/checks/integration.sh

echo ""
echo "=== checks passed ==="
