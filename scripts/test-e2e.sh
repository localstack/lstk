#!/usr/bin/env bash
# Run the TypeScript end-to-end suite against the built binary (bin/lstk).
#
# Requires Node >= 26 (the suite is type-erasable TypeScript, run through vitest)
# and pnpm. See test/e2e/README.md.
#
# Honors:
#   CREATE_JUNIT_REPORT  Emit JUnit XML to test-e2e-results.xml when set.
#   SHARD_INDEX          1-based shard index (used with SHARD_TOTAL).
#   SHARD_TOTAL          Total number of shards; passed to vitest --shard.
#   RUN                  Substring filter passed to vitest -t.

set -euo pipefail

cd "$(dirname "$0")/../test/e2e"

if ! command -v pnpm >/dev/null 2>&1; then
  echo "pnpm is required to run the e2e suite: https://pnpm.io/installation" >&2
  exit 1
fi

NODE_MAJOR=$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0)
if [ "$NODE_MAJOR" -lt 26 ]; then
  echo "Node >= 26 is required to run the e2e suite (found $(node -v 2>/dev/null || echo none))." >&2
  echo "The suite is type-erasable TypeScript targeting Node's native type stripping." >&2
  exit 1
fi

if [ -n "${CI:-}" ]; then
  pnpm install --frozen-lockfile
else
  # A full install on every local run is slow; install only when deps are missing.
  [ -d node_modules ] || pnpm install
fi

ARGS=()
if [ -n "${SHARD_TOTAL:-}" ]; then
  ARGS+=(--shard "${SHARD_INDEX:-1}/${SHARD_TOTAL}")
fi
if [ -n "${RUN:-}" ]; then
  ARGS+=(-t "$RUN")
fi

exec pnpm exec vitest run ${ARGS[@]+"${ARGS[@]}"}
