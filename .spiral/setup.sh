#!/bin/sh
# Spiral boot script for lstk. Spiral runs it with /bin/sh, as root, once per
# boot from the checkout root (spiral: internal/worker/worker.go, runSetup); the
# repo image bake runs the same script with open egress and snapshots the
# result. The AMI is a cache, so every step must stay correct on a stale image.
# Keep it POSIX sh and idempotent.
#
#   toolchain.sh   Go from go.mod, golangci-lint from .tool-versions, the OpenSpec CLI.
#   workspace.sh   both modules' dependencies and a warm build cache.
#
# Exports here do NOT reach agent turns or check.sh: the driver hands each a
# fresh environment, so what must persist goes on disk or into /etc/profile.d.
# shellcheck source=setup/lib.sh
. "$(dirname "$0")/setup/lib.sh"

sh .spiral/setup/toolchain.sh
sh .spiral/setup/workspace.sh

log "ready in $root"
