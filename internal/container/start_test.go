package container

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStart_ReturnsEarlyIfRuntimeUnhealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().Healthy(gomock.Any()).Return(errors.New("cannot connect to Docker daemon"))

	var buf bytes.Buffer
	sink := output.NewPlainSink(&buf)

	err := Start(context.Background(), mockRT, sink, nil, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime not healthy")
	assert.Contains(t, buf.String(), "Docker is not available")
	assert.True(t, output.IsSilent(err), "error should be silent since it was already emitted")
}
