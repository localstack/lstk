#!/bin/sh
# Piece of .spiral/setup.sh: both Go modules' dependencies (test/integration is
# its own module), and a warm build cache for the gate's first run. Only a
# go.sum that moved past the image reaches proxy.golang.org at boot.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

log "go mod download (root and test/integration)"
go mod download
(cd test/integration && go mod download)

log "warming the build cache"
go build ./...
go test -count=1 -run '^$' ./... >/dev/null
(cd test/integration && go test -count=1 -run '^$' ./... >/dev/null)
