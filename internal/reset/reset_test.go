package reset_test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/reset"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var awsContainers = []config.ContainerConfig{{Type: config.EmulatorAWS}}

type recordedEvents struct {
	mu     sync.Mutex
	events []output.Event
}

func (r *recordedEvents) snapshot() []output.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]output.Event(nil), r.events...)
}

// captureEvents returns a sink that records every event and a prompts channel
// that yields each UserInputRequestEvent for tests to respond to.
func captureEvents() (output.Sink, *recordedEvents, <-chan output.UserInputRequestEvent) {
	rec := &recordedEvents{}
	prompts := make(chan output.UserInputRequestEvent, 4)
	sink := output.SinkFunc(func(event output.Event) {
		rec.mu.Lock()
		rec.events = append(rec.events, event)
		rec.mu.Unlock()
		if req, ok := event.(output.UserInputRequestEvent); ok {
			prompts <- req
		}
	})
	return sink, rec, prompts
}

func healthyRunningMock(t *testing.T) *runtime.MockRuntime {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	return mockRT
}

func TestReset_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	resetter := NewMockStateResetter(ctrl)
	resetter.EXPECT().ResetState(gomock.Any(), "host:4566").Return(nil)

	sink, rec, _ := captureEvents()

	err := reset.Reset(context.Background(), healthyRunningMock(t), awsContainers, resetter, "host:4566", true, sink)
	require.NoError(t, err)

	var spinnerStarted, spinnerStopped, succeeded bool
	for _, e := range rec.snapshot() {
		switch ev := e.(type) {
		case output.SpinnerEvent:
			if ev.Active {
				spinnerStarted = true
			} else {
				spinnerStopped = true
			}
		case output.EmulatorResetEvent:
			succeeded = true
			assert.Equal(t, "aws", ev.Type)
		}
	}
	assert.True(t, spinnerStarted, "spinner should have started")
	assert.True(t, spinnerStopped, "spinner should have stopped")
	assert.True(t, succeeded, "EmulatorResetEvent should have been emitted")
}

func TestReset_EmulatorNotRunning(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	resetter := NewMockStateResetter(ctrl)
	sink, rec, _ := captureEvents()

	err := reset.Reset(context.Background(), mockRT, awsContainers, resetter, "host:4566", true, sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))

	var gotErrorEvent bool
	for _, e := range rec.snapshot() {
		if ev, ok := e.(output.ErrorEvent); ok {
			gotErrorEvent = true
			assert.Contains(t, ev.Title, "not running")
			assert.NotEmpty(t, ev.Actions)
		}
	}
	assert.True(t, gotErrorEvent, "ErrorEvent should have been emitted")
}

func TestReset_UnhealthyRuntime(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(fmt.Errorf("docker unavailable"))
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), gomock.Any())

	resetter := NewMockStateResetter(ctrl)
	sink := output.NewPlainSink(io.Discard)

	err := reset.Reset(context.Background(), mockRT, awsContainers, resetter, "host:4566", true, sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}

func TestReset_ResetterError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	resetter := NewMockStateResetter(ctrl)
	resetter.EXPECT().ResetState(gomock.Any(), gomock.Any()).Return(fmt.Errorf("connection refused"))
	sink := output.NewPlainSink(io.Discard)

	err := reset.Reset(context.Background(), healthyRunningMock(t), awsContainers, resetter, "host:4566", true, sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestReset_ConfirmYes(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	resetter := NewMockStateResetter(ctrl)
	resetter.EXPECT().ResetState(gomock.Any(), gomock.Any()).Return(nil)

	sink, _, prompts := captureEvents()

	go func() {
		req := <-prompts
		req.ResponseCh <- output.InputResponse{SelectedKey: "y"}
	}()

	err := reset.Reset(context.Background(), healthyRunningMock(t), awsContainers, resetter, "host:4566", false, sink)
	require.NoError(t, err)
}

func TestReset_ConfirmNo(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	resetter := NewMockStateResetter(ctrl)

	sink, rec, prompts := captureEvents()

	go func() {
		req := <-prompts
		req.ResponseCh <- output.InputResponse{SelectedKey: "n"}
	}()

	err := reset.Reset(context.Background(), healthyRunningMock(t), awsContainers, resetter, "host:4566", false, sink)
	require.NoError(t, err)

	var cancelled bool
	for _, e := range rec.snapshot() {
		if ev, ok := e.(output.MessageEvent); ok && ev.Severity == output.SeverityNote && ev.Text == "Cancelled" {
			cancelled = true
		}
	}
	assert.True(t, cancelled, "cancellation message should have been emitted")
}
