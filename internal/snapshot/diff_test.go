package snapshot_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestDiffPod_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	differ := NewMockPodDiffer(ctrl)
	differ.EXPECT().DiffPodSnapshot(gomock.Any(), gomock.Any(), "my-baseline", 0, "test-token").
		Return(snapshot.DiffResult{
			"s3":       {Additions: 5},
			"sqs":      {Additions: 3, Modifications: 1},
			"dynamodb": {Additions: 1},
		}, nil)

	sink, getEvents := captureEvents(t)
	err := snapshot.DiffPod(context.Background(), healthyRunningMock(t), awsContainers, differ, "", "my-baseline", 0, "test-token", snapshot.MergeStrategyAccountRegion, sink)
	require.NoError(t, err)

	events := getEvents()
	var spinnerStarted, spinnerStopped bool
	var diffEvent *output.SnapshotDiffEvent
	for _, e := range events {
		switch ev := e.(type) {
		case output.SpinnerEvent:
			if ev.Active {
				spinnerStarted = true
			} else {
				spinnerStopped = true
			}
		case output.SnapshotDiffEvent:
			diffEvent = &ev
		}
	}
	assert.True(t, spinnerStarted, "spinner should have started")
	assert.True(t, spinnerStopped, "spinner should have stopped")
	require.NotNil(t, diffEvent, "SnapshotDiffEvent should have been emitted")
	assert.Equal(t, "my-baseline", diffEvent.PodName)
	assert.Equal(t, snapshot.MergeStrategyAccountRegion, diffEvent.Strategy)
	assert.Equal(t, 5, diffEvent.Services["s3"].Additions)
	assert.Equal(t, 3, diffEvent.Services["sqs"].Additions)
	assert.Equal(t, 1, diffEvent.Services["sqs"].Modifications)
}

func TestDiffPod_PinnedVersion(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	differ := NewMockPodDiffer(ctrl)
	differ.EXPECT().DiffPodSnapshot(gomock.Any(), gomock.Any(), "my-baseline", 3, "test-token").
		Return(snapshot.DiffResult{"s3": {Additions: 2}}, nil)

	sink, getEvents := captureEvents(t)
	err := snapshot.DiffPod(context.Background(), healthyRunningMock(t), awsContainers, differ, "", "my-baseline", 3, "test-token", snapshot.MergeStrategyAccountRegion, sink)
	require.NoError(t, err)

	var diffEvent *output.SnapshotDiffEvent
	for _, e := range getEvents() {
		if ev, ok := e.(output.SnapshotDiffEvent); ok {
			diffEvent = &ev
		}
	}
	require.NotNil(t, diffEvent, "SnapshotDiffEvent should have been emitted")
	assert.Equal(t, "my-baseline", diffEvent.PodName)
	assert.Equal(t, 3, diffEvent.Version)
}

func TestDiffPod_VersionNotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	differ := NewMockPodDiffer(ctrl)
	const serverMsg = "Unable to load pod my-baseline with version 9. The maximum version available in the remote storage is 3"
	differ.EXPECT().DiffPodSnapshot(gomock.Any(), gomock.Any(), "my-baseline", 9, "test-token").
		Return(nil, fmt.Errorf("%w: %s", snapshot.ErrPodVersionNotFound, serverMsg))

	sink, getEvents := captureEvents(t)
	err := snapshot.DiffPod(context.Background(), healthyRunningMock(t), awsContainers, differ, "", "my-baseline", 9, "test-token", "", sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))

	var errEvent *output.ErrorEvent
	for _, e := range getEvents() {
		if ev, ok := e.(output.ErrorEvent); ok {
			errEvent = &ev
		}
	}
	require.NotNil(t, errEvent, "a structured ErrorEvent should have been emitted")
	assert.Equal(t, "Could not check pod diff", errEvent.Title)
	assert.Equal(t, serverMsg, errEvent.Summary)
	require.NotEmpty(t, errEvent.Actions)
	assert.Equal(t, "lstk snapshot versions pod:my-baseline", errEvent.Actions[0].Value)
}

func TestDiffPod_EmptyResult(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	differ := NewMockPodDiffer(ctrl)
	differ.EXPECT().DiffPodSnapshot(gomock.Any(), gomock.Any(), "empty-pod", 0, "test-token").
		Return(snapshot.DiffResult{}, nil)

	sink, getEvents := captureEvents(t)
	err := snapshot.DiffPod(context.Background(), healthyRunningMock(t), awsContainers, differ, "", "empty-pod", 0, "test-token", snapshot.MergeStrategyAccountRegion, sink)
	require.NoError(t, err)

	var diffEvent *output.SnapshotDiffEvent
	for _, e := range getEvents() {
		if ev, ok := e.(output.SnapshotDiffEvent); ok {
			diffEvent = &ev
		}
	}
	require.NotNil(t, diffEvent, "SnapshotDiffEvent should have been emitted even for empty result")
	assert.Empty(t, diffEvent.Services)
}

func TestDiffPod_NoAuthToken(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	differ := NewMockPodDiffer(ctrl)
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.DiffPod(context.Background(), runtime.NewMockRuntime(ctrl), awsContainers, differ, "", "my-baseline", 0, "", "", sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication")
}

func TestDiffPod_DifferError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	differ := NewMockPodDiffer(ctrl)
	differ.EXPECT().DiffPodSnapshot(gomock.Any(), gomock.Any(), "my-baseline", 0, "test-token").
		Return(nil, fmt.Errorf("platform unreachable"))

	sink, _ := captureEvents(t)
	err := snapshot.DiffPod(context.Background(), healthyRunningMock(t), awsContainers, differ, "", "my-baseline", 0, "test-token", "", sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform unreachable")
}

func TestDiffPod_PodNotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	differ := NewMockPodDiffer(ctrl)
	differ.EXPECT().DiffPodSnapshot(gomock.Any(), gomock.Any(), "does-not-exist", 0, "test-token").
		Return(nil, fmt.Errorf("%w: Failed to get version information from platform.. aborting", snapshot.ErrPodNotFound))

	sink, getEvents := captureEvents(t)
	err := snapshot.DiffPod(context.Background(), healthyRunningMock(t), awsContainers, differ, "", "does-not-exist", 0, "test-token", "", sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))

	var gotErrorEvent bool
	for _, e := range getEvents() {
		if ev, ok := e.(output.ErrorEvent); ok {
			gotErrorEvent = true
			assert.Contains(t, ev.Summary, "not found on the LocalStack platform")
			assert.NotContains(t, ev.Summary, "version information")
		}
	}
	assert.True(t, gotErrorEvent, "ErrorEvent should have been emitted")
}

func TestDiffPod_EmulatorNotRunning(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	differ := NewMockPodDiffer(ctrl)
	sink, getEvents := captureEvents(t)

	err := snapshot.DiffPod(context.Background(), mockRT, awsContainers, differ, "", "my-baseline", 0, "test-token", "", sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))

	var gotErrorEvent bool
	for _, e := range getEvents() {
		if ev, ok := e.(output.ErrorEvent); ok {
			gotErrorEvent = true
			assert.Contains(t, ev.Title, "not running")
		}
	}
	assert.True(t, gotErrorEvent, "ErrorEvent should have been emitted")
}

func TestDiffPod_UnhealthyRuntime(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(fmt.Errorf("docker unavailable"))
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), gomock.Any())

	differ := NewMockPodDiffer(ctrl)
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.DiffPod(context.Background(), mockRT, awsContainers, differ, "", "my-baseline", 0, "test-token", "", sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}
