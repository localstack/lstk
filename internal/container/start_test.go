package container

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestResolveContainerVersions_PinnedTagIsUnchanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockPlatform := api.NewMockPlatformAPI(ctrl)
	// API must not be called for pinned tags
	containers := []runtime.ContainerConfig{
		{Tag: "3.8.1", Image: "localstack/localstack-pro:3.8.1", EmulatorType: "aws"},
	}

	result := resolveContainerVersions(context.Background(), mockPlatform, containers)

	assert.Equal(t, "3.8.1", result[0].Tag)
	assert.Equal(t, "localstack/localstack-pro:3.8.1", result[0].Image)
}

func TestResolveContainerVersions_ResolvesLatestToSpecificVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockPlatform := api.NewMockPlatformAPI(ctrl)
	mockPlatform.EXPECT().GetLatestCatalogVersion(gomock.Any(), "aws").Return("3.8.1", nil)
	containers := []runtime.ContainerConfig{
		{Tag: "latest", Image: "localstack/localstack-pro:latest", EmulatorType: "aws"},
	}

	result := resolveContainerVersions(context.Background(), mockPlatform, containers)

	assert.Equal(t, "3.8.1", result[0].Tag)
	assert.Equal(t, "localstack/localstack-pro:3.8.1", result[0].Image)
}

func TestResolveContainerVersions_KeepsLatestWhenAPIFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockPlatform := api.NewMockPlatformAPI(ctrl)
	mockPlatform.EXPECT().GetLatestCatalogVersion(gomock.Any(), "aws").Return("", errors.New("api down"))
	containers := []runtime.ContainerConfig{
		{Tag: "latest", Image: "localstack/localstack-pro:latest", EmulatorType: "aws"},
	}

	result := resolveContainerVersions(context.Background(), mockPlatform, containers)

	assert.Equal(t, "latest", result[0].Tag)
	assert.Equal(t, "localstack/localstack-pro:latest", result[0].Image)
}

func TestResolveVersionsFromImages_PinnedTagIsUnchanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	// GetImageVersion must not be called for pinned tags
	containers := []runtime.ContainerConfig{
		{Tag: "3.8.1", Image: "localstack/localstack-pro:3.8.1"},
	}

	result, err := resolveVersionsFromImages(context.Background(), mockRT, containers)

	require.NoError(t, err)
	assert.Equal(t, "3.8.1", result[0].Tag)
}

func TestResolveVersionsFromImages_ResolvesLatestViaImageInspection(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().GetImageVersion(gomock.Any(), "localstack/localstack-pro:latest").Return("3.8.1", nil)
	containers := []runtime.ContainerConfig{
		{Tag: "latest", Image: "localstack/localstack-pro:latest"},
	}

	result, err := resolveVersionsFromImages(context.Background(), mockRT, containers)

	require.NoError(t, err)
	assert.Equal(t, "3.8.1", result[0].Tag)
}

func TestResolveVersionsFromImages_ReturnsErrorWhenImageInspectionFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().GetImageVersion(gomock.Any(), "localstack/localstack-pro:latest").
		Return("", errors.New("image not found"))
	containers := []runtime.ContainerConfig{
		{Tag: "latest", Image: "localstack/localstack-pro:latest"},
	}

	_, err := resolveVersionsFromImages(context.Background(), mockRT, containers)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "image not found")
}

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
