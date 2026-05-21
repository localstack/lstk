package snapshot_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func nopStarter(ctx context.Context, sink output.Sink) error { return nil }

func mockLocalClientReturning(t *testing.T, importErr error) *MockLocalLoadClient {
	t.Helper()
	ctrl := gomock.NewController(t)
	m := NewMockLocalLoadClient(ctrl)
	if importErr == nil {
		m.EXPECT().ImportState(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	} else {
		m.EXPECT().ImportState(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(importErr).AnyTimes()
	}
	return m
}

func writeSnapshotFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.zip")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func TestLoadLocal_Success(t *testing.T) {
	t.Parallel()
	src := writeSnapshotFile(t, "ZIP_DATA")
	client := mockLocalClientReturning(t, nil)
	sink, getEvents := captureEvents(t)

	err := snapshot.LoadLocal(context.Background(), healthyRunningMock(t), awsContainers, client, "", src, "", nopStarter, sink)
	require.NoError(t, err)

	events := getEvents()
	var spinnerStarted, spinnerStopped, loaded bool
	for _, e := range events {
		switch ev := e.(type) {
		case output.SpinnerEvent:
			if ev.Active {
				spinnerStarted = true
			} else {
				spinnerStopped = true
			}
		case output.SnapshotLoadedEvent:
			loaded = true
			assert.NotEmpty(t, ev.Source)
		}
	}
	assert.True(t, spinnerStarted, "spinner should have started")
	assert.True(t, spinnerStopped, "spinner should have stopped")
	assert.True(t, loaded, "SnapshotLoadedEvent should have been emitted")
}

func TestLoadLocal_OverwriteStrategy(t *testing.T) {
	t.Parallel()
	src := writeSnapshotFile(t, "ZIP_DATA")

	ctrl := gomock.NewController(t)
	client := NewMockLocalLoadClient(ctrl)
	client.EXPECT().ResetState(gomock.Any(), gomock.Any()).Return(nil)
	client.EXPECT().ImportState(gomock.Any(), gomock.Any(), gomock.Any(), "").Return(nil)

	sink := output.NewPlainSink(io.Discard)
	err := snapshot.LoadLocal(context.Background(), healthyRunningMock(t), awsContainers, client, "", src, snapshot.MergeStrategyOverwrite, nopStarter, sink)
	require.NoError(t, err)
}

func TestLoadLocal_ResetErrorAbortsImport(t *testing.T) {
	t.Parallel()
	src := writeSnapshotFile(t, "ZIP_DATA")

	ctrl := gomock.NewController(t)
	client := NewMockLocalLoadClient(ctrl)
	client.EXPECT().ResetState(gomock.Any(), gomock.Any()).Return(fmt.Errorf("reset failed"))

	sink := output.NewPlainSink(io.Discard)
	err := snapshot.LoadLocal(context.Background(), healthyRunningMock(t), awsContainers, client, "", src, snapshot.MergeStrategyOverwrite, nopStarter, sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reset failed")
}

func TestLoadLocal_ImportError(t *testing.T) {
	t.Parallel()
	src := writeSnapshotFile(t, "ZIP_DATA")
	client := mockLocalClientReturning(t, fmt.Errorf("incompatible version"))
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.LoadLocal(context.Background(), healthyRunningMock(t), awsContainers, client, "", src, "", nopStarter, sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incompatible version")
}

func TestLoadLocal_FileNotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	client := NewMockLocalLoadClient(ctrl)
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.LoadLocal(context.Background(), healthyRunningMock(t), awsContainers, client, "", "/no/such/file.zip", "", nopStarter, sink)
	require.Error(t, err)
}

func TestLoadLocal_EmulatorNotRunning_AutoStarts(t *testing.T) {
	t.Parallel()
	src := writeSnapshotFile(t, "ZIP_DATA")

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	client := NewMockLocalLoadClient(ctrl)
	client.EXPECT().ImportState(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	var starterCalled bool
	starter := func(ctx context.Context, sink output.Sink) error {
		starterCalled = true
		return nil
	}

	sink := output.NewPlainSink(io.Discard)
	err := snapshot.LoadLocal(context.Background(), mockRT, awsContainers, client, "", src, "", starter, sink)
	require.NoError(t, err)
	assert.True(t, starterCalled, "starter should have been called when emulator is not running")
}

func TestLoadLocal_EmulatorNotRunning_NoStarter(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	client := NewMockLocalLoadClient(ctrl)
	src := writeSnapshotFile(t, "ZIP_DATA")
	sink, getEvents := captureEvents(t)

	err := snapshot.LoadLocal(context.Background(), mockRT, awsContainers, client, "", src, "", nil, sink)
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

func TestLoadLocal_UnhealthyRuntime(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(fmt.Errorf("docker unavailable"))
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), gomock.Any())

	client := NewMockLocalLoadClient(ctrl)
	src := writeSnapshotFile(t, "ZIP_DATA")
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.LoadLocal(context.Background(), mockRT, awsContainers, client, "", src, "", nopStarter, sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}

func TestLoadPod_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	loader := NewMockPodLoader(ctrl)
	loader.EXPECT().LoadPodSnapshot(gomock.Any(), gomock.Any(), "my-baseline", "test-token", "").
		Return([]string{"s3", "dynamodb"}, nil)

	sink, getEvents := captureEvents(t)
	err := snapshot.LoadPod(context.Background(), healthyRunningMock(t), awsContainers, loader, "", "my-baseline", "test-token", "", nopStarter, sink)
	require.NoError(t, err)

	events := getEvents()
	var spinnerStarted, spinnerStopped bool
	var loaded *output.SnapshotLoadedEvent
	for _, e := range events {
		switch ev := e.(type) {
		case output.SpinnerEvent:
			if ev.Active {
				spinnerStarted = true
			} else {
				spinnerStopped = true
			}
		case output.SnapshotLoadedEvent:
			loaded = &ev
		}
	}
	assert.True(t, spinnerStarted, "spinner should have started")
	assert.True(t, spinnerStopped, "spinner should have stopped")
	require.NotNil(t, loaded, "SnapshotLoadedEvent should have been emitted")
	assert.Equal(t, "pod:my-baseline", loaded.Source)
	assert.Equal(t, []string{"s3", "dynamodb"}, loaded.Services)
}

func TestLoadPod_NoAuthToken(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	loader := NewMockPodLoader(ctrl)
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.LoadPod(context.Background(), runtime.NewMockRuntime(ctrl), awsContainers, loader, "", "my-baseline", "", "", nopStarter, sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication")
}

func TestLoadPod_LoaderError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	loader := NewMockPodLoader(ctrl)
	loader.EXPECT().LoadPodSnapshot(gomock.Any(), gomock.Any(), "my-baseline", "test-token", gomock.Any()).
		Return(nil, fmt.Errorf("platform unreachable"))

	sink, _ := captureEvents(t)
	err := snapshot.LoadPod(context.Background(), healthyRunningMock(t), awsContainers, loader, "", "my-baseline", "test-token", "", nopStarter, sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform unreachable")
}

func TestLoadPod_WithMergeStrategy(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	loader := NewMockPodLoader(ctrl)
	loader.EXPECT().LoadPodSnapshot(gomock.Any(), gomock.Any(), "my-pod", "tok", snapshot.MergeStrategyService).
		Return([]string{"s3"}, nil)

	sink := output.NewPlainSink(io.Discard)
	err := snapshot.LoadPod(context.Background(), healthyRunningMock(t), awsContainers, loader, "", "my-pod", "tok", snapshot.MergeStrategyService, nopStarter, sink)
	require.NoError(t, err)
}

func TestLoadPod_EmulatorNotRunning_AutoStarts(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	loader := NewMockPodLoader(ctrl)
	loader.EXPECT().LoadPodSnapshot(gomock.Any(), gomock.Any(), "my-pod", "tok", gomock.Any()).
		Return([]string{"s3"}, nil)

	var starterCalled bool
	starter := func(ctx context.Context, sink output.Sink) error {
		starterCalled = true
		return nil
	}

	sink := output.NewPlainSink(io.Discard)
	err := snapshot.LoadPod(context.Background(), mockRT, awsContainers, loader, "", "my-pod", "tok", "", starter, sink)
	require.NoError(t, err)
	assert.True(t, starterCalled, "starter should have been called when emulator is not running")
}
