//go:build windows

package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
)

// npipeProbeTimeout mirrors probeSocket's unix-domain dial timeout.
const npipeProbeTimeout = 200 * time.Millisecond

// probeNamedPipe reports whether the Windows named pipe encoded in a
// "npipe://" endpoint (e.g. "npipe:////./pipe/docker_engine") is actually
// reachable, so a stale Windows Docker CLI context is skipped the same way a
// stale unix socket context is (see probeSocket).
func probeNamedPipe(npipeEndpoint string) bool {
	addr := strings.TrimPrefix(npipeEndpoint, "npipe://")
	ctx, cancel := context.WithTimeout(context.Background(), npipeProbeTimeout)
	defer cancel()
	conn, err := winio.DialPipeContext(ctx, addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
