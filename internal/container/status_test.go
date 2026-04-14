package container

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStatus_IsRunningError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, fmt.Errorf("docker unavailable"))

	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	sink := output.NewPlainSink(io.Discard)

	err := Status(context.Background(), mockRT, containers, "", nil, sink)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker unavailable")
}

func TestStatus_MultipleContainers_StopsAtFirstNotRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), "localstack/localstack-pro", "4566/tcp", gomock.Any()).Return(nil, nil)

	containers := []config.ContainerConfig{
		{Type: config.EmulatorAWS},
		{Type: config.EmulatorSnowflake},
	}
	sink := output.NewPlainSink(io.Discard)

	err := Status(context.Background(), mockRT, containers, "", nil, sink)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}
