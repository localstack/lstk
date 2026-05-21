package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
)

// SnapshotLoadClient is satisfied by aws.Client.
type SnapshotLoadClient interface {
	snapshot.LocalLoadClient
	snapshot.PodLoader
}

func RunSnapshotLoad(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client SnapshotLoadClient, host string, src snapshot.Destination, authToken, strategy string, starter snapshot.Starter) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		switch src.Kind {
		case snapshot.KindPod:
			return snapshot.LoadPod(ctx, rt, containers, client, host, src.Value, authToken, strategy, starter, sink)
		default:
			return snapshot.LoadLocal(ctx, rt, containers, client, host, src.Value, strategy, starter, sink)
		}
	})
}
