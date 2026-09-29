#!/usr/bin/env bash
# Tests for scripts/check-bundled-packaging-sync.sh — the guard that keeps the
# packaging half (.goreleaser.yaml) and the download half (the release job in
# ci.yml) from being merged separately, and the bundle's licence-document list
# identical across the fetch script, .goreleaser.yaml, the npm script and the
# updater. All fixtures are passed in as file paths, so the suite never depends
# on the repo's current wiring.
set -euo pipefail

SUITE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/bundled-extensions/tests/lib.sh
. "${SUITE_DIR}/lib.sh"

CHECK="${SUITE_DIR}/../check-bundled-packaging-sync.sh"

# $3 is the value stamped into internal/version.bundlesExtensions; "none" omits
# the ldflag altogether. DOCS adds one `src: bundled/<name>` entry per staged
# document name to the live layout.
write_goreleaser() {
  local path="$1" mode="$2" flag="${3:-none}" doc
  {
    echo "builds:"
    echo "  - id: lstk"
    echo "    ldflags:"
    if [ "${flag}" = "none" ]; then
      echo "      - -s -w -X github.com/localstack/lstk/internal/version.version={{ .Version }}"
    else
      echo "      - -s -w -X github.com/localstack/lstk/internal/version.version={{ .Version }} -X github.com/localstack/lstk/internal/version.bundlesExtensions=${flag}"
    fi
  } > "${path}"
  case "${mode}" in
    live)
      cat >> "${path}" <<'YAML'
archives:
  - id: lstk
    files:
      - completions/*
      - src: "bundled/{{ .Os }}_{{ .Arch }}/lstk-*"
        strip_parent: true
YAML
      for doc in ${DOCS-}; do
        printf '      - src: bundled/%s\n        strip_parent: true\n' "${doc}" >> "${path}"
      done
      ;;
    commented)
      cat >> "${path}" <<'YAML'
archives:
  - id: lstk
    files:
      - completions/*
      # - src: "bundled/{{ .Os }}_{{ .Arch }}/lstk-*"
      #   strip_parent: true
YAML
      ;;
    absent)
      cat >> "${path}" <<'YAML'
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

# The three other places the document list lives: the fetch script (bare
# archive names), the npm script (staged names) and the updater (staged names
# as Go string literals, allowed to run ahead).
write_lists() {
  local fetch_docs="$1" npm_docs="$2" go_docs="$3" name
  printf 'BUNDLED_BINARY="bundled-extensions"\nBUNDLE_DOCUMENTS="%s"\n' "${fetch_docs}" > "${FETCH}"
  printf 'STAGED_BUNDLE_DOCUMENTS="%s"\n' "${npm_docs}" > "${NPM}"
  {
    echo 'package update'
    echo 'func names() []string {'
    echo '	return []string{'
    for name in ${go_docs}; do echo "		\"${name}\","; done
    echo '	}'
    echo '}'
  } > "${GO}"
}

WORK="$(mktemp -d)"
GOR="${WORK}/goreleaser.yaml"
WF="${WORK}/ci.yml"
FETCH="${WORK}/fetch.sh"
NPM="${WORK}/npm.sh"
GO="${WORK}/extract.go"

echo "== check-bundled-packaging-sync.sh =="
write_lists "" "" ""

begin_test "both halves absent: in step, passes"
write_goreleaser "${GOR}" absent false
write_workflow "${WF}" none
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_ok

begin_test "both halves present: in step, passes"
write_goreleaser "${GOR}" live true
write_workflow "${WF}" fetch
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_ok

begin_test "packaging without the fetch step fails"
write_goreleaser "${GOR}" live true
write_workflow "${WF}" none
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails
assert_output_contains "fetch-bundled-extensions.sh"

begin_test "the fetch step without packaging fails"
write_goreleaser "${GOR}" absent false
write_workflow "${WF}" fetch
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails
assert_output_contains "bundled/"

begin_test "a commented-out packaging entry does not count as live"
write_goreleaser "${GOR}" commented false
write_workflow "${WF}" none
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_ok

begin_test "a commented-out fetch step does not count as wired"
write_goreleaser "${GOR}" live true
write_workflow "${WF}" commented
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails

begin_test "a fetch step in another job does not satisfy the release job"
write_goreleaser "${GOR}" live true
write_workflow "${WF}" other-job
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails

# The build flag (internal/version.bundlesExtensions) tells a release binary to
# expect the bundle beside it: it gates the reinstall hints. It has to agree
# with whether packaging ships one.
begin_test "packaging live but the build flag off fails"
write_goreleaser "${GOR}" live false
write_workflow "${WF}" fetch
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails
assert_output_contains "bundlesExtensions"

begin_test "packaging live but the build flag missing fails"
write_goreleaser "${GOR}" live none
write_workflow "${WF}" fetch
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails
assert_output_contains "bundlesExtensions"

begin_test "build flag on without packaging fails"
write_goreleaser "${GOR}" absent true
write_workflow "${WF}" none
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails
assert_output_contains "bundlesExtensions"

begin_test "no packaging and no build flag at all is in step"
write_goreleaser "${GOR}" absent none
write_workflow "${WF}" none
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_ok

begin_test "identical document lists in all four places pass"
DOCS="bundled-extensions.THIRD_PARTY_NOTICES.txt" write_goreleaser "${GOR}" live true
write_workflow "${WF}" fetch
write_lists "THIRD_PARTY_NOTICES.txt" "bundled-extensions.THIRD_PARTY_NOTICES.txt" "bundled-extensions.THIRD_PARTY_NOTICES.txt bundled-extensions.LICENSE.txt"
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_ok

begin_test "a document the fetch script stages but the archives do not ship fails"
write_goreleaser "${GOR}" live true
write_workflow "${WF}" fetch
write_lists "THIRD_PARTY_NOTICES.txt" "" "bundled-extensions.THIRD_PARTY_NOTICES.txt"
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails
assert_output_contains "bundled-extensions.THIRD_PARTY_NOTICES.txt"
assert_output_contains "goreleaser"

begin_test "a document the archives ship but the npm packages do not fails"
DOCS="bundled-extensions.THIRD_PARTY_NOTICES.txt" write_goreleaser "${GOR}" live true
write_workflow "${WF}" fetch
write_lists "THIRD_PARTY_NOTICES.txt" "" "bundled-extensions.THIRD_PARTY_NOTICES.txt"
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails
assert_output_contains "bundled-extensions.THIRD_PARTY_NOTICES.txt"
assert_output_contains "npm"

begin_test "a shipped document the updater would not install fails"
DOCS="bundled-extensions.THIRD_PARTY_NOTICES.txt" write_goreleaser "${GOR}" live true
write_workflow "${WF}" fetch
write_lists "THIRD_PARTY_NOTICES.txt" "bundled-extensions.THIRD_PARTY_NOTICES.txt" ""
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_fails
assert_output_contains "bundled-extensions.THIRD_PARTY_NOTICES.txt"
assert_output_contains "extract.go"

begin_test "the updater may name a document nobody ships yet"
DOCS="bundled-extensions.THIRD_PARTY_NOTICES.txt" write_goreleaser "${GOR}" live true
write_workflow "${WF}" fetch
write_lists "THIRD_PARTY_NOTICES.txt" "bundled-extensions.THIRD_PARTY_NOTICES.txt" "bundled-extensions.THIRD_PARTY_NOTICES.txt bundled-extensions.LICENSE.txt"
run_script "${CHECK}" "${GOR}" "${WF}" "${FETCH}" "${NPM}" "${GO}"
assert_ok

begin_test "the repo's own files are in step"
run_script "${CHECK}"
assert_ok

finish_suite
