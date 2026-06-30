package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotSaveRemoteS3(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client snapshot.RemoteClient, host, podName, s3URL string, creds snapshot.S3Credentials, authToken string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.SaveRemoteS3(ctx, rt, containers, client, host, podName, s3URL, creds, authToken, sink)
	})
}

func RunSnapshotLoadRemoteS3(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client snapshot.RemoteClient, host, podName, s3URL string, creds snapshot.S3Credentials, authToken, strategy string, starter snapshot.Starter) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.LoadRemoteS3(ctx, rt, containers, client, host, podName, s3URL, creds, authToken, strategy, starter, sink)
	})
}

func RunSnapshotListRemoteS3(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client snapshot.RemoteClient, host, s3URL string, creds snapshot.S3Credentials, authToken string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.ListRemoteS3(ctx, rt, containers, client, host, s3URL, creds, authToken, sink)
	})
}
