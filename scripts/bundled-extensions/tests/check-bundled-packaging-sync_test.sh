#!/usr/bin/env bash
# Tests for scripts/check-bundled-packaging-sync.sh — the guard that keeps the
# packaging half (.goreleaser.yaml) and the download half (the release job in
# ci.yml) from being merged separately. Both fixtures are passed in as file
# paths, so the suite never depends on the repo's current wiring.
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/bundled-extensions/tests/lib.sh
. "${SUITE_DIR}/lib.sh"

CHECK="${SUITE_DIR}/../check-bundled-packaging-sync.sh"

write_goreleaser() {
  local path="$1" mode="$2"
  case "${mode}" in
    live)
      cat > "${path}" <<'YAML'
archives:
  - id: lstk
    files:
      - completions/*
      - src: "bundled/{{ .Os }}_{{ .Arch }}/lstk-*"
        strip_parent: true
YAML
      ;;
    commented)
      cat > "${path}" <<'YAML'
archives:
  - id: lstk
    files:
      - completions/*
      # - src: "bundled/{{ .Os }}_{{ .Arch }}/lstk-*"
      #   strip_parent: true
YAML
      ;;
    absent)
      cat > "${path}" <<'YAML'
archives:
  - id: lstk
    files:
      - completions/*
YAML
      ;;
  esac
}

write_workflow() {
  local path="$1" mode="$2"
  case "${mode}" in
    fetch)
      cat > "${path}" <<'YAML'
jobs:
  test-unit:
    steps:
      - run: make test
  release:
    steps:
      - name: Fetch bundled extensions
        run: scripts/fetch-bundled-extensions.sh
      - name: Run GoReleaser
        run: goreleaser release --clean
YAML
      ;;
    none)
      cat > "${path}" <<'YAML'
jobs:
  test-unit:
    steps:
      - run: make test
  release:
    steps:
      - name: Run GoReleaser
        run: goreleaser release --clean
YAML
      ;;
    commented)
      cat > "${path}" <<'YAML'
jobs:
  release:
    steps:
      # - name: Fetch bundled extensions
      #   run: scripts/fetch-bundled-extensions.sh
      - name: Run GoReleaser
        run: goreleaser release --clean
YAML
      ;;
    other-job)
      cat > "${path}" <<'YAML'
jobs:
  some-other-job:
    steps:
      - run: scripts/fetch-bundled-extensions.sh
  release:
    steps:
      - name: Run GoReleaser
        run: goreleaser release --clean
YAML
      ;;
  esac
}

WORK="$(mktemp -d)"
GOR="${WORK}/goreleaser.yaml"
WF="${WORK}/ci.yml"

echo "== check-bundled-packaging-sync.sh =="

begin_test "both halves absent: in step, passes"
write_goreleaser "${GOR}" absent
write_workflow "${WF}" none
run_script "${CHECK}" "${GOR}" "${WF}"
assert_ok

begin_test "both halves present: in step, passes"
write_goreleaser "${GOR}" live
write_workflow "${WF}" fetch
run_script "${CHECK}" "${GOR}" "${WF}"
assert_ok

begin_test "packaging without the fetch step fails"
write_goreleaser "${GOR}" live
write_workflow "${WF}" none
run_script "${CHECK}" "${GOR}" "${WF}"
assert_fails
assert_output_contains "fetch-bundled-extensions.sh"

begin_test "the fetch step without packaging fails"
write_goreleaser "${GOR}" absent
write_workflow "${WF}" fetch
run_script "${CHECK}" "${GOR}" "${WF}"
assert_fails
assert_output_contains "bundled/"

begin_test "a commented-out packaging entry does not count as live"
write_goreleaser "${GOR}" commented
write_workflow "${WF}" none
run_script "${CHECK}" "${GOR}" "${WF}"
assert_ok

begin_test "a commented-out fetch step does not count as wired"
write_goreleaser "${GOR}" live
write_workflow "${WF}" commented
run_script "${CHECK}" "${GOR}" "${WF}"
assert_fails

begin_test "a fetch step in another job does not satisfy the release job"
write_goreleaser "${GOR}" live
write_workflow "${WF}" other-job
run_script "${CHECK}" "${GOR}" "${WF}"
assert_fails

begin_test "the repo's own files are in step"
run_script "${CHECK}"
assert_ok

finish_suite
