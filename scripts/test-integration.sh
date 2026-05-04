#!/usr/bin/env bash
# Run integration tests, optionally sharded across CI runners.
#
# Honors:
#   CREATE_JUNIT_REPORT  Emit JUnit XML when set.
#   SHARD_INDEX          1-based shard index (used with SHARD_TOTAL).
#   SHARD_TOTAL          Total number of shards. When set, this run executes
#                        only the tests assigned to SHARD_INDEX (round-robin
#                        partition by sorted test name).
#   RUN                  Override: pass directly to `go test -run`.
#
# Sharding is disabled when SHARD_TOTAL is unset or empty, so the script also
# acts as a drop-in replacement for the unsharded `go test ./...` invocation.

set -euo pipefail

cd "$(dirname "$0")/../test/integration"

JUNIT_FLAG=()
if [ -n "${CREATE_JUNIT_REPORT:-}" ]; then
  JUNIT_FLAG=(--junitfile ../../test-integration-results.xml)
fi

if [ "$(uname)" = "Darwin" ]; then
  export LSTK_KEYRING=file
fi

RUN_FLAG=()
if [ -n "${SHARD_TOTAL:-}" ]; then
  IDX="${SHARD_INDEX:-1}"
  # `go test -list` enumerates top-level tests in each package. Run it as a
  # standalone step (no stderr suppression) so compile errors surface loudly
  # under `set -e` instead of being swallowed into an empty test list. The
  # subsequent `go test -run` reuses the compiled test binary from Go's build
  # cache, so this isn't a duplicate compile.
  LIST_OUTPUT=$(go test -list '.*' ./...)
  # Filter to lines matching /^Test/ to drop the trailing "ok pkg" summary
  # lines, sort for stable partitioning, and pick every N-th entry.
  # SHARD_INDEX is 1-based for human-friendly CI labels (shard 1/4, 2/4, ...).
  TESTS=$(echo "$LIST_OUTPUT" | awk '/^Test/' | sort \
    | awk -v idx="$IDX" -v total="$SHARD_TOTAL" '((NR-1) % total) + 1 == idx')
  if [ -z "$TESTS" ]; then
    echo "no tests for shard $IDX/$SHARD_TOTAL — skipping"
    exit 0
  fi
  REGEX="^($(echo "$TESTS" | paste -sd '|' -))\$"
  RUN_FLAG=(-run "$REGEX")
elif [ -n "${RUN:-}" ]; then
  RUN_FLAG=(-run "$RUN")
fi

# Bash 3 (macOS default) treats `${arr[@]}` on an empty array as unbound under
# `set -u`. The `${arr[@]+"${arr[@]}"}` idiom expands to nothing when the
# array is empty and to the array's contents otherwise.
exec go run gotest.tools/gotestsum@latest --format testname \
  ${JUNIT_FLAG[@]+"${JUNIT_FLAG[@]}"} \
  -- -count=1 -timeout 15m \
  ${RUN_FLAG[@]+"${RUN_FLAG[@]}"} \
  ./...
