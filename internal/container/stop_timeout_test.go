package container

import (
	"context"
	"errors"
	"testing"

	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

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
		{"the last value wins, as in Docker", []string{"SHUTDOWN_TIMEOUT=20", "SHUTDOWN_TIMEOUT= 30 "}, nil, nil, 35},
		{"without one, the supervisor's default", nil, []string{"PATH=/bin"}, nil, 10},
		{"an image that cannot be read has none", nil, nil, errors.New("no such image"), 10},
		{"a value that is no positive number counts as none", []string{"SHUTDOWN_TIMEOUT=soon"}, []string{"SHUTDOWN_TIMEOUT=0"}, nil, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockRT := runtime.NewMockRuntime(ctrl)
			mockRT.EXPECT().ImageEnv(gomock.Any(), image).Return(tc.imageEnv, tc.imageErr).AnyTimes()

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
