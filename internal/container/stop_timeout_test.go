package container

import (
	"context"
	"errors"
	"testing"

	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestStopTimeoutFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want int
	}{
		{"without one, the supervisor's default plus the margin", []string{"PATH=/bin"}, 10},
		{"SHUTDOWN_TIMEOUT plus the margin", []string{"SHUTDOWN_TIMEOUT=60"}, 65},
		{"blanks around the value are ignored, as the supervisor strips them", []string{"SHUTDOWN_TIMEOUT= 30 "}, 35},
		{"a blank value is the supervisor's default", []string{"SHUTDOWN_TIMEOUT="}, 10},
		{"the last value wins, as in the container", []string{"SHUTDOWN_TIMEOUT=60", "SHUTDOWN_TIMEOUT=20"}, 25},
		{"LOCALSTACK_SHUTDOWN_TIMEOUT wins, as the entrypoint re-exports it", []string{"SHUTDOWN_TIMEOUT=20", "LOCALSTACK_SHUTDOWN_TIMEOUT=120"}, 125},
		{"LOCALSTACK_SHUTDOWN_TIMEOUT wins wherever it is", []string{"LOCALSTACK_SHUTDOWN_TIMEOUT=120", "SHUTDOWN_TIMEOUT=20"}, 125},
		{"a blank LOCALSTACK_SHUTDOWN_TIMEOUT blanks SHUTDOWN_TIMEOUT too", []string{"SHUTDOWN_TIMEOUT=60", "LOCALSTACK_SHUTDOWN_TIMEOUT="}, 10},
		{"0, which tells the supervisor never to kill, gets the bounded no-kill timeout", []string{"SHUTDOWN_TIMEOUT=0"}, noKillStopTimeout},
		{"so does a negative value", []string{"SHUTDOWN_TIMEOUT=-1"}, noKillStopTimeout},
		{"a value the supervisor cannot start with gets the default", []string{"SHUTDOWN_TIMEOUT=soon"}, 10},
		{"a value beyond any real wait is bounded", []string{"SHUTDOWN_TIMEOUT=99999999999"}, maxStopTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, stopTimeoutFromEnv(tc.env))
		})
	}
}

func TestStopTimeoutFor(t *testing.T) {
	const image = "localstack/localstack-azure:latest"
	tests := []struct {
		name     string
		env      []string
		imageEnv []string
		imageErr error
		want     int
	}{
		{"the image's SHUTDOWN_TIMEOUT plus the margin", nil, []string{"PATH=/bin", "SHUTDOWN_TIMEOUT=60"}, nil, 65},
		{"the container's value wins over the image's", []string{"SHUTDOWN_TIMEOUT=20"}, []string{"SHUTDOWN_TIMEOUT=60"}, nil, 25},
		{"the container's 0 wins over the image's value", []string{"SHUTDOWN_TIMEOUT=0"}, []string{"SHUTDOWN_TIMEOUT=60"}, nil, noKillStopTimeout},
		{"a forwarded LOCALSTACK_SHUTDOWN_TIMEOUT wins over the image's value", []string{"LOCALSTACK_SHUTDOWN_TIMEOUT=120"}, []string{"SHUTDOWN_TIMEOUT=60"}, nil, 125},
		{"the image's LOCALSTACK_SHUTDOWN_TIMEOUT wins over the container's SHUTDOWN_TIMEOUT", []string{"SHUTDOWN_TIMEOUT=20"}, []string{"LOCALSTACK_SHUTDOWN_TIMEOUT=40"}, nil, 45},
		{"without one, the supervisor's default", nil, []string{"PATH=/bin"}, nil, 10},
		{"an image that cannot be read leaves the container's value", []string{"SHUTDOWN_TIMEOUT=20"}, nil, errors.New("no such image"), 25},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockRT := runtime.NewMockRuntime(ctrl)
			mockRT.EXPECT().ImageEnv(gomock.Any(), image).Return(tc.imageEnv, tc.imageErr)

			got := stopTimeoutFor(context.Background(), mockRT, runtime.ContainerConfig{Image: image, Env: tc.env})

			assert.Equal(t, tc.want, got)
		})
	}
}

// withDefaultStopTimeout is c as the start flow hands it to the runtime when
// neither the container nor the image sets SHUTDOWN_TIMEOUT.
func withDefaultStopTimeout(c runtime.ContainerConfig) runtime.ContainerConfig {
	c.StopTimeout = defaultShutdownTimeout + stopTimeoutMargin
	return c
}
