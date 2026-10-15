package container

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func seconds(n int) *int { return &n }

func TestStop_GivesDockerTheEmulatorsShutdownTime(t *testing.T) {
	inspectFailed := errors.New("inspect failed")
	tests := []struct {
		name        string
		brief       runtime.ContainerBrief
		inspectErr  error
		env         []string
		envErr      error
		wantTimeout *int
		wantWait    time.Duration
	}{
		{
			// Docker waits that long for the emulator to shut down: a shorter wait here would report a failed stop
			// while the emulator is still removing what it started.
			name:        "the container's own stop timeout",
			brief:       runtime.ContainerBrief{Exists: true, Running: true, StopTimeout: seconds(65)},
			wantTimeout: seconds(65),
			wantWait:    65*time.Second + stopWaitMargin,
		},
		{
			// A container an older lstk created, or one started outside lstk, would otherwise get Docker's 10 s.
			name:        "one derived from the environment of a container without its own",
			brief:       runtime.ContainerBrief{Exists: true, Running: true},
			env:         []string{"PATH=/bin", "SHUTDOWN_TIMEOUT=60"},
			wantTimeout: seconds(65),
			wantWait:    65*time.Second + stopWaitMargin,
		},
		{
			// Podman's Docker API reports its 10 s default for every container.
			name:        "the environment's when the container's own is shorter",
			brief:       runtime.ContainerBrief{Exists: true, Running: true, StopTimeout: seconds(10)},
			env:         []string{"SHUTDOWN_TIMEOUT=60"},
			wantTimeout: seconds(65),
			wantWait:    65*time.Second + stopWaitMargin,
		},
		{
			name:        "the container's own when it is longer than the environment's",
			brief:       runtime.ContainerBrief{Exists: true, Running: true, StopTimeout: seconds(120)},
			env:         []string{"SHUTDOWN_TIMEOUT=60"},
			wantTimeout: seconds(120),
			wantWait:    120*time.Second + stopWaitMargin,
		},
		{
			name:        "a bounded one for a container's own beyond any real wait",
			brief:       runtime.ContainerBrief{Exists: true, Running: true, StopTimeout: seconds(math.MaxInt)},
			wantTimeout: seconds(maxStopTimeout),
			wantWait:    maxStopTimeout*time.Second + stopWaitMargin,
		},
		{
			name:        "the supervisor's default for an environment without one",
			brief:       runtime.ContainerBrief{Exists: true, Running: true},
			env:         []string{"PATH=/bin"},
			wantTimeout: seconds(10),
			wantWait:    minStopWait,
		},
		{
			name:        "a bounded one instead of a container's wait forever",
			brief:       runtime.ContainerBrief{Exists: true, Running: true, StopTimeout: seconds(-1)},
			wantTimeout: seconds(noKillStopTimeout),
			wantWait:    noKillStopTimeout*time.Second + stopWaitMargin,
		},
		{
			name:        "the environment's when the container cannot be inspected",
			inspectErr:  inspectFailed,
			env:         []string{"SHUTDOWN_TIMEOUT=20"},
			wantTimeout: seconds(25),
			wantWait:    25*time.Second + stopWaitMargin,
		},
		{
			name:       "Docker's own when the container cannot be read at all",
			inspectErr: inspectFailed,
			envErr:     errors.New("env failed"),
			wantWait:   minStopWait,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockRT := runtime.NewMockRuntime(ctrl)
			mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
			mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-azure").Return(true, nil)
			mockRT.EXPECT().InspectBrief(gomock.Any(), "localstack-azure").Return(tc.brief, tc.inspectErr)
			mockRT.EXPECT().ContainerEnv(gomock.Any(), "localstack-azure").Return(tc.env, tc.envErr).AnyTimes()
			var gotTimeout *int
			var gotWait time.Duration
			mockRT.EXPECT().Stop(gomock.Any(), "localstack-azure", gomock.Any()).DoAndReturn(
				func(ctx context.Context, _ string, timeout *int) error {
					deadline, ok := ctx.Deadline()
					require.True(t, ok, "the stop must run under a deadline")
					gotWait = time.Until(deadline)
					gotTimeout = timeout
					return nil
				})
			tel, _ := newCapturingTelClient(t)
			t.Cleanup(tel.Close)

			// Port 1: nothing answers there, so the info lookup for telemetry fails.
			containers := []config.ContainerConfig{{Type: config.EmulatorAzure, Port: "1"}}
			require.NoError(t, Stop(context.Background(), mockRT, output.NewPlainSink(io.Discard), containers, StopOptions{Telemetry: tel}))

			assert.Equal(t, tc.wantTimeout, gotTimeout)
			assert.InDelta(t, tc.wantWait.Seconds(), gotWait.Seconds(), 1)
		})
	}
}
