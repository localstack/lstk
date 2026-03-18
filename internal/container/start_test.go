package container

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStart_ReturnsEarlyIfRuntimeUnhealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(errors.New("cannot connect to Docker daemon"))
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), gomock.Any())

	sink := output.NewPlainSink(io.Discard)

	err := Start(context.Background(), mockRT, sink, StartOptions{Logger: log.Nop()}, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime not healthy")
	assert.True(t, output.IsSilent(err), "error should be silent since it was already emitted")
}

func TestEmitPostStartPointers_WithWebApp(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, "localhost.localstack.cloud:4566", "https://app.localstack.cloud/")

	got := out.String()
	assert.Contains(t, got, "• Endpoint: localhost.localstack.cloud:4566\n")
	assert.Contains(t, got, "• Web app: https://app.localstack.cloud\n")
	assert.Contains(t, got, "> Tip:")
}

func TestEmitPostStartPointers_WithoutWebApp(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, "127.0.0.1:4566", "")

	got := out.String()
	assert.Contains(t, got, "• Endpoint: 127.0.0.1:4566\n")
	assert.Contains(t, got, "> Tip:")
}

func TestServicePortRange_Returns50Entries(t *testing.T) {
	ports := servicePortRange()

	require.Len(t, ports, 50)
	assert.Equal(t, "4510", ports[0].ContainerPort)
	assert.Equal(t, "4510", ports[0].HostPort)
	assert.Equal(t, "4559", ports[49].ContainerPort)
	assert.Equal(t, "4559", ports[49].HostPort)

	for i, p := range ports {
		expected := strconv.Itoa(4510 + i)
		assert.Equal(t, expected, p.ContainerPort)
		assert.Equal(t, expected, p.HostPort)
	}
}
