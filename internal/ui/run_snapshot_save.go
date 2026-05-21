package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
)

// SnapshotClient is satisfied by any type that can both export local state and
// save a remote pod snapshot — aws.Client implements both today.
type SnapshotClient interface {
	snapshot.StateExporter
	snapshot.PodSaver
}

func RunSnapshotSave(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client SnapshotClient, host string, dest snapshot.Destination, authToken string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		switch dest.Kind {
		case snapshot.KindPod:
			return snapshot.SavePod(ctx, rt, containers, client, host, dest.Value, authToken, sink)
		default:
			return snapshot.SaveLocal(ctx, rt, containers, client, host, dest.Value, sink)
		}
	})
}
