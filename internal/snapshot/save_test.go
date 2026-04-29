package snapshot_test

import (
	"bytes"
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

// fakeExporter implements StateExporter for tests.
type fakeExporter struct {
	body []byte
	err  error
}

func (f *fakeExporter) ExportState(_ context.Context) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(bytes.NewReader(f.body)), nil
}

func captureEvents(t *testing.T) (output.Sink, func() []any) {
	t.Helper()
	var events []any
	sink := output.SinkFunc(func(event any) {
		events = append(events, event)
	})
	return sink, func() []any { return events }
}

func healthyRunningMock(t *testing.T) *runtime.MockRuntime {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	return mockRT
}

var awsContainers = []config.ContainerConfig{{Type: config.EmulatorAWS}}

func TestSave_Success(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	exporter := &fakeExporter{body: []byte("ZIP_DATA")}
	sink, getEvents := captureEvents(t)

	err := snapshot.Save(context.Background(), healthyRunningMock(t), awsContainers, exporter, dest, sink)
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
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	sink, getEvents := captureEvents(t)

	err := snapshot.Save(context.Background(), mockRT, awsContainers, &fakeExporter{body: []byte("x")}, dest, sink)
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
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(fmt.Errorf("docker unavailable"))
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), gomock.Any())

	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.Save(context.Background(), mockRT, awsContainers, &fakeExporter{}, dest, sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}

func TestSave_ExporterError(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	exporter := &fakeExporter{err: fmt.Errorf("connection refused")}
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.Save(context.Background(), healthyRunningMock(t), awsContainers, exporter, dest, sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")

	_, statErr := os.Stat(filepath.Join(dir, "snap"))
	assert.True(t, os.IsNotExist(statErr), "no file should be created on exporter error")
}

func TestSave_DestinationDirNotExist(t *testing.T) {
	dest := "/no/such/dir/snap"
	exporter := &fakeExporter{body: []byte("ZIP_DATA")}
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.Save(context.Background(), healthyRunningMock(t), awsContainers, exporter, dest, sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save to")
}

func TestSave_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap")
	require.NoError(t, os.WriteFile(path, []byte("OLD"), 0600))

	dest := path
	exporter := &fakeExporter{body: []byte("NEW")}
	sink := output.NewPlainSink(io.Discard)

	err := snapshot.Save(context.Background(), healthyRunningMock(t), awsContainers, exporter, dest, sink)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "NEW", string(data))
}

func TestSave_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "snap")
	exporter := &fakeExporter{err: ctx.Err()}

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), gomock.Any()).Return(true, nil)

	sink := output.NewPlainSink(io.Discard)

	err := snapshot.Save(ctx, mockRT, awsContainers, exporter, dest, sink)
	require.Error(t, err)
}
