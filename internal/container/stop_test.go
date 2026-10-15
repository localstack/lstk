package container

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// stopDeadline stops a running Azure emulator whose inspection returns brief and err, and returns the deadline
// lstk gave Docker's stop.
func stopDeadline(t *testing.T, brief runtime.ContainerBrief, err error) time.Duration {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-azure").Return(true, nil)
	mockRT.EXPECT().InspectBrief(gomock.Any(), "localstack-azure").Return(brief, err)
	var deadline time.Time
	mockRT.EXPECT().Stop(gomock.Any(), "localstack-azure").DoAndReturn(func(ctx context.Context, _ string) error {
		deadline, _ = ctx.Deadline()
		return nil
	})
	tel, _ := newCapturingTelClient(t)
	t.Cleanup(tel.Close)

	started := time.Now()
	// Port 1: nothing answers there, so the info lookup for telemetry fails at once.
	containers := []config.ContainerConfig{{Type: config.EmulatorAzure, Port: "1"}}
	require.NoError(t, Stop(context.Background(), mockRT, output.NewPlainSink(io.Discard), containers, StopOptions{Telemetry: tel}))
	require.False(t, deadline.IsZero(), "the stop must run under a deadline")
	return deadline.Sub(started)
}

func TestStop_WaitsForTheContainersOwnStopTimeout(t *testing.T) {
	// Docker waits the container's stop timeout for the emulator to shut down: a shorter wait here would report a
	// failed stop while the emulator is still removing what it started.
	wait := stopDeadline(t, runtime.ContainerBrief{Exists: true, Running: true, StopTimeout: 65}, nil)

	assert.InDelta(t, (65*time.Second + stopWaitMargin).Seconds(), wait.Seconds(), 2)
}

func TestStop_KeepsItsDefaultWaitForAContainerWithoutAStopTimeout(t *testing.T) {
	wait := stopDeadline(t, runtime.ContainerBrief{Exists: true, Running: true}, nil)

	assert.InDelta(t, minStopWait.Seconds(), wait.Seconds(), 2)
}

func TestStop_KeepsItsDefaultWaitWhenTheContainerCannotBeInspected(t *testing.T) {
	wait := stopDeadline(t, runtime.ContainerBrief{}, errors.New("inspect failed"))

	assert.InDelta(t, minStopWait.Seconds(), wait.Seconds(), 2)
}
