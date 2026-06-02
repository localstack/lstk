package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/tracing"
)

// runFlushTelemetry handles the flusher subprocess. It bypasses the normal
// Execute() boot path — no logger, keyring, telemetry client, or cobra tree —
// so it writes nothing to disk and cannot spawn another flusher.
func runFlushTelemetry(ctx context.Context, args []string) error {
	cfg := env.Init()
	if cfg.TracesEnabled {
		shutdown := tracing.Init(ctx, log.Nop())
		defer func() {
			// Fresh context: ctx may be cancelled and Shutdown would skip the flush.
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(shutCtx)
		}()
	}

	endpoint := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--endpoint" && i+1 < len(args) {
			endpoint = args[i+1]
			i++
		}
	}
	if endpoint == "" {
		return fmt.Errorf("missing --endpoint")
	}

	ctx = tracing.ContextWithRemoteParent(ctx, os.Getenv)
	return telemetry.RunFlush(ctx, endpoint, os.Stdin)
}
