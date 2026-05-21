package snapshot_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func captureEvents(t *testing.T) (output.Sink, func() []output.Event) {
	t.Helper()
	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
	})
	return sink, func() []output.Event { return events }
}

func healthyRunningMock(t *testing.T) *runtime.MockRuntime {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	return mockRT
}

func mockExporterReturning(t *testing.T, body []byte) *MockStateExporter {
	t.Helper()
	ctrl := gomock.NewController(t)
	m := NewMockStateExporter(ctrl)
	m.EXPECT().ExportState(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, dst io.Writer) error {
			_, err := dst.Write(body)
			return err
		},
	)
	return m
}

func mockExporterReturningError(t *testing.T, exportErr error) *MockStateExporter {
	t.Helper()
	ctrl := gomock.NewController(t)
	m := NewMockStateExporter(ctrl)
	m.EXPECT().ExportState(gomock.Any(), gomock.Any(), gomock.Any()).Return(exportErr)
	return m
}

var awsContainers = []config.ContainerConfig{{Type: config.EmulatorAWS}}

func TestSave_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	exporter := mockExporterReturning(t, []byte("ZIP_DATA"))
	sink, getEvents := captureEvents(t)

	err := snapshot.SaveLocal(context.Background(), healthyRunningMock(t), awsContainers, exporter, "", dest, sink)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "snap"))
	require.NoError(t, err)
	assert.Equal(t, "ZIP_DATA", string(data))

	events := getEvents()
	require.NotEmpty(t, events)

	var spinnerStarted, spinnerStopped, succeeded bool
	for _, e := range events {
		switch ev := e.(type) {
		case output.SpinnerEvent:
			if ev.Active {
				spinnerStarted = true
			} else {
				spinnerStopped = true
			}
		case output.MessageEvent:
			if ev.Severity == output.SeveritySuccess {
				succeeded = true
				assert.Contains(t, ev.Text, dest)
			}
		}
	}
	assert.True(t, spinnerStarted, "spinner should have started")
	assert.True(t, spinnerStopped, "spinner should have stopped")
	assert.True(t, succeeded, "success event should have been emitted")
}

func TestSave_EmulatorNotRunning(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	exporter := NewMockStateExporter(ctrl)

	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	sink, getEvents := captureEvents(t)

	err := snapshot.SaveLocal(context.Background(), mockRT, awsContainers, exporter, "", dest, sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))

	var gotErrorEvent bool
	for _, e := range getEvents() {
		if ev, ok := e.(output.ErrorEvent); ok {
			gotErrorEvent = true
			assert.Contains(t, ev.Title, "not running")
			assert.NotEmpty(t, ev.Actions)
		}
	}
	assert.True(t, gotErrorEvent, "ErrorEvent should have been emitted")

	_, statErr := os.Stat(filepath.Join(dir, "snap"))
	assert.True(t, os.IsNotExist(statErr), "no file should be created when emulator is not running")
}

func TestSave_UnhealthyRuntime(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(fmt.Errorf("docker unavailable"))
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), gomock.Any())

	exporter := NewMockStateExporter(ctrl)

	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.SaveLocal(context.Background(), mockRT, awsContainers, exporter, "", dest, sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}

func TestSave_ExporterError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	exporter := mockExporterReturningError(t, fmt.Errorf("connection refused"))
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.SaveLocal(context.Background(), healthyRunningMock(t), awsContainers, exporter, "", dest, sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")

	_, statErr := os.Stat(filepath.Join(dir, "snap"))
	assert.True(t, os.IsNotExist(statErr), "no file should be created on exporter error")
}

func TestSave_DestinationDirNotExist(t *testing.T) {
	t.Parallel()
	dest := "/no/such/dir/snap"
	ctrl := gomock.NewController(t)
	exporter := NewMockStateExporter(ctrl)
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.SaveLocal(context.Background(), healthyRunningMock(t), awsContainers, exporter, "", dest, sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save to")
}

func TestSave_OverwritesExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "snap")
	require.NoError(t, os.WriteFile(path, []byte("OLD"), 0600))

	dest := path
	exporter := mockExporterReturning(t, []byte("NEW"))
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.SaveLocal(context.Background(), healthyRunningMock(t), awsContainers, exporter, "", dest, sink)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "NEW", string(data))
}

func TestSave_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	exporter := mockExporterReturningError(t, ctx.Err())

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), gomock.Any()).Return(true, nil)

	sink := output.NewPlainSink(io.Discard)

	err := snapshot.SaveLocal(ctx, mockRT, awsContainers, exporter, "", dest, sink)
	require.Error(t, err)
}

func TestSavePod_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	saver := NewMockPodSaver(ctrl)
	saver.EXPECT().SavePodSnapshot(gomock.Any(), gomock.Any(), "my-baseline", "test-token").Return(
		snapshot.PodSaveResult{Version: 2, Services: []string{"dynamodb", "s3"}, Size: 1048576},
		nil,
	)

	sink, getEvents := captureEvents(t)
	err := snapshot.SavePod(context.Background(), healthyRunningMock(t), awsContainers, saver, "", "my-baseline", "test-token", sink)
	require.NoError(t, err)

	events := getEvents()
	var spinnerStarted, spinnerStopped bool
	var saved *output.PodSnapshotSavedEvent
	for _, e := range events {
		switch ev := e.(type) {
		case output.SpinnerEvent:
			if ev.Active {
				spinnerStarted = true
			} else {
				spinnerStopped = true
			}
		case output.PodSnapshotSavedEvent:
			saved = &ev
		}
	}
	assert.True(t, spinnerStarted, "spinner should have started")
	assert.True(t, spinnerStopped, "spinner should have stopped")
	require.NotNil(t, saved, "PodSnapshotSavedEvent should have been emitted")
	assert.Equal(t, "my-baseline", saved.PodName)
	assert.Equal(t, 2, saved.Version)
	assert.Equal(t, []string{"dynamodb", "s3"}, saved.Services)
	assert.Equal(t, int64(1048576), saved.Size)
}

func TestSavePod_NoAuthToken(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	saver := NewMockPodSaver(ctrl)

	sink := output.NewPlainSink(io.Discard)
	err := snapshot.SavePod(context.Background(), runtime.NewMockRuntime(ctrl), awsContainers, saver, "", "my-baseline", "", sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication")
}

func TestSavePod_EmulatorNotRunning(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	saver := NewMockPodSaver(ctrl)
	sink, getEvents := captureEvents(t)

	err := snapshot.SavePod(context.Background(), mockRT, awsContainers, saver, "", "my-baseline", "test-token", sink)
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

func TestSavePod_UnhealthyRuntime(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(fmt.Errorf("docker unavailable"))
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), gomock.Any())

	saver := NewMockPodSaver(ctrl)
	sink, _ := captureEvents(t)

	err := snapshot.SavePod(context.Background(), mockRT, awsContainers, saver, "", "my-baseline", "test-token", sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}

func TestSavePod_SaverError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	saver := NewMockPodSaver(ctrl)
	saver.EXPECT().SavePodSnapshot(gomock.Any(), gomock.Any(), "my-baseline", "test-token").Return(snapshot.PodSaveResult{}, fmt.Errorf("platform unreachable"))

	sink, _ := captureEvents(t)
	err := snapshot.SavePod(context.Background(), healthyRunningMock(t), awsContainers, saver, "", "my-baseline", "test-token", sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform unreachable")
}

