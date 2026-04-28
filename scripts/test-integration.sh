#!/usr/bin/env bash
# Run integration tests, optionally sharded across CI runners.
#
# Honors:
#   CREATE_JUNIT_REPORT  Emit JUnit XML when set.
#   SHARD_INDEX          0-based shard index (used with SHARD_TOTAL).
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
  IDX="${SHARD_INDEX:-0}"
  # `go test -list` enumerates top-level tests in each package; we filter to
  # lines matching /^Test/ to drop the trailing "ok pkg" summary lines, sort
  # for stable partitioning, and pick every N-th entry.
  TESTS=$(go test -list '.*' ./... 2>/dev/null | awk '/^Test/' | sort \
    | awk -v idx="$IDX" -v total="$SHARD_TOTAL" '(NR-1) % total == idx')
  if [ -z "$TESTS" ]; then
    echo "no tests for shard $IDX/$SHARD_TOTAL — skipping"
    exit 0
  fi
  REGEX="^($(echo "$TESTS" | paste -sd '|' -))\$"
  RUN_FLAG=(-run "$REGEX")
elif [ -n "${RUN:-}" ]; then
  RUN_FLAG=(-run "$RUN")
fi

exec go run gotest.tools/gotestsum@latest --format testname "${JUNIT_FLAG[@]}" \
  -- -count=1 -timeout 15m "${RUN_FLAG[@]}" ./...
