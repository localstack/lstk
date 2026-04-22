package container

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func TestServicePortRange_ReturnsExpectedPorts(t *testing.T) {
	ports := servicePortRange()

	require.Len(t, ports, 51)
	assert.Equal(t, "443", ports[0].ContainerPort)
	assert.Equal(t, "443", ports[0].HostPort)
	assert.Equal(t, "4510", ports[1].ContainerPort)
	assert.Equal(t, "4510", ports[1].HostPort)
	assert.Equal(t, "4559", ports[50].ContainerPort)
	assert.Equal(t, "4559", ports[50].HostPort)
}

func TestFilterHostEnv(t *testing.T) {
	input := []string{
		"CI=true",
		"LOCALSTACK_DISABLE_EVENTS=1",
		"LOCALSTACK_API_ENDPOINT=https://example.test",
		"LOCALSTACK_AUTH_TOKEN=host-token",
		"PATH=/usr/bin",
		"HOME=/home/user",
		"CI_PIPELINE=foo",
	}

	got := filterHostEnv(input)

	assert.Contains(t, got, "CI=true")
	assert.Contains(t, got, "LOCALSTACK_DISABLE_EVENTS=1")
	assert.Contains(t, got, "LOCALSTACK_API_ENDPOINT=https://example.test")
	assert.NotContains(t, got, "LOCALSTACK_AUTH_TOKEN=host-token",
		"host LOCALSTACK_AUTH_TOKEN must be filtered so it cannot overwrite the lstk-resolved token")
	assert.NotContains(t, got, "PATH=/usr/bin")
	assert.NotContains(t, got, "HOME=/home/user")
	assert.NotContains(t, got, "CI_PIPELINE=foo", "only exact CI= must be forwarded, not CI_*")
}

