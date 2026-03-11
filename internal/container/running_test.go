package container

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestAnyRunning_ReturnsTrueWhenConfiguredContainerIsRunning(t *testing.T) {
	initTestConfig(t, `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
`)

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)

	running, err := AnyRunning(context.Background(), mockRT)

	require.NoError(t, err)
	assert.True(t, running)
}

func TestAnyRunning_ReturnsFalseWhenConfiguredContainerIsNotRunning(t *testing.T) {
	initTestConfig(t, `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
`)

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)

	running, err := AnyRunning(context.Background(), mockRT)

	require.NoError(t, err)
	assert.False(t, running)
}

func initTestConfig(t *testing.T, content string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err)
	require.NoError(t, config.InitFromPath(path))
	t.Cleanup(func() {
		require.NoError(t, config.InitFromPath(path))
	})
}
