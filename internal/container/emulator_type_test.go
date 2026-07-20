package container

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// loadTempConfig writes content to a temp config.toml and loads it as the active
// config, returning its path. These tests mutate the process-global viper state,
// so they must not run in parallel.
func loadTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, config.InitFromPath(path))
	return path
}

// mockRuntimeNothingRunning returns a MockRuntime whose FindRunningByImage
// reports nothing running on the queried port, for tests that exercise the
// switch/first-run paths without caring about the conflict guard itself.
func mockRuntimeNothingRunning(t *testing.T) runtime.Runtime {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	return mockRT
}

func TestApplyEmulatorType_SwitchesInPlace(t *testing.T) {
	path := loadTempConfig(t, "[[containers]]\ntype = \"aws\"     # keep me\ntag = \"latest\"\nport = \"4566\"\n")
	cfg, err := config.Get()
	require.NoError(t, err)

	var buf bytes.Buffer
	containers, err := ApplyEmulatorType(context.Background(), mockRuntimeNothingRunning(t), output.NewPlainSink(&buf), config.EmulatorAzure, cfg.Containers, false, path)
	require.NoError(t, err)

	require.Len(t, containers, 1)
	assert.Equal(t, config.EmulatorAzure, containers[0].Type)
	assert.Contains(t, buf.String(), "Switched configured emulator to Azure")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "azure"`)
	assert.Contains(t, string(data), "# keep me")
}

func TestApplyEmulatorType_NoOpWhenMatching(t *testing.T) {
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"
	path := loadTempConfig(t, content)
	cfg, err := config.Get()
	require.NoError(t, err)

	var buf bytes.Buffer
	// No FindRunningByImage expectation: matching types return before the
	// conflict guard runs, so rt is never touched.
	containers, err := ApplyEmulatorType(context.Background(), nil, output.NewPlainSink(&buf), config.EmulatorAWS, cfg.Containers, false, path)
	require.NoError(t, err)

	assert.Equal(t, config.EmulatorAWS, containers[0].Type)
	assert.Empty(t, buf.String())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestApplyEmulatorType_ErrorsWhenImageSet(t *testing.T) {
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\nimage = \"my-registry.example.com/localstack-pro:3.0\"\n"
	path := loadTempConfig(t, content)
	cfg, err := config.Get()
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = ApplyEmulatorType(context.Background(), mockRuntimeNothingRunning(t), output.NewPlainSink(&buf), config.EmulatorSnowflake, cfg.Containers, false, path)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
	assert.Contains(t, buf.String(), "custom image")

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}

// TestApplyEmulatorType_ErrorsWhenNoContainersBlock exercises the defensive
// guard against a container-less config. config.Get() normally injects a default
// container, so we pass an explicitly empty slice to reach the branch that would
// otherwise panic on containers[0]; it must surface a clear, silent error.
func TestApplyEmulatorType_ErrorsWhenNoContainersBlock(t *testing.T) {
	path := loadTempConfig(t, "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n")

	var buf bytes.Buffer
	// No FindRunningByImage expectation: the empty-containers guard returns
	// before the conflict guard runs, so rt is never touched.
	_, err := ApplyEmulatorType(context.Background(), nil, output.NewPlainSink(&buf), config.EmulatorAzure, nil, false, path)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
	assert.Contains(t, buf.String(), "[[containers]] block")
}

func TestApplyEmulatorType_WarnsOnTagAndVolumes(t *testing.T) {
	path := loadTempConfig(t, "[[containers]]\ntype = \"aws\"\ntag = \"3.0\"\nport = \"4566\"\nvolumes = [\"./init.sql:/etc/localstack/init/ready.d/init.sql\"]\n")
	cfg, err := config.Get()
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = ApplyEmulatorType(context.Background(), mockRuntimeNothingRunning(t), output.NewPlainSink(&buf), config.EmulatorSnowflake, cfg.Containers, false, path)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `Keeping tag "3.0"`)
	assert.Contains(t, out, "Keeping volume mounts")
	assert.Contains(t, out, "Switched configured emulator to Snowflake")
}

// TestApplyEmulatorType_RefusesSwitchWhenDifferentEmulatorRunning is the
// regression for the bug where `lstk -t snowflake` rewrote the config to
// Snowflake even though Azure was still running on the port, leaving `status`/
// `stop`/`logs` unable to find the actually-running Azure emulator. The switch
// must be refused, and the config left untouched, when a different emulator is
// already running on the port the requested type would use.
func TestApplyEmulatorType_RefusesSwitchWhenDifferentEmulatorRunning(t *testing.T) {
	content := "[[containers]]\ntype = \"azure\"\ntag = \"latest\"\nport = \"4566\"\n"
	path := loadTempConfig(t, content)
	cfg, err := config.Get()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), config.KnownImageRepos(), "4566/tcp").
		Return(&runtime.RunningContainer{Name: "localstack-azure", Image: "localstack/localstack-azure:latest", BoundPort: "4566"}, nil)

	var buf bytes.Buffer
	_, err = ApplyEmulatorType(context.Background(), mockRT, output.NewPlainSink(&buf), config.EmulatorSnowflake, cfg.Containers, false, path)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))

	out := buf.String()
	assert.Contains(t, out, "LocalStack Azure Emulator is running on port 4566")
	assert.Contains(t, out, "config was not changed")
	assert.Contains(t, out, "docker stop localstack-azure")

	// Config must be left untouched: still Azure, not rewritten to Snowflake.
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}

// TestApplyEmulatorType_AllowsSwitchWhenSameTypeAlreadyRunning pins that the
// conflict guard only blocks a *different* running type — switching to a type
// that happens to already be running (e.g. re-selecting it) must proceed
// normally; selectContainersToStart's own "already running" handling takes it
// from there.
func TestApplyEmulatorType_AllowsSwitchWhenSameTypeAlreadyRunning(t *testing.T) {
	path := loadTempConfig(t, "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n")
	cfg, err := config.Get()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), config.KnownImageRepos(), "4566/tcp").
		Return(&runtime.RunningContainer{Name: "localstack-snowflake", Image: "localstack/snowflake:latest", BoundPort: "4566"}, nil)

	var buf bytes.Buffer
	containers, err := ApplyEmulatorType(context.Background(), mockRT, output.NewPlainSink(&buf), config.EmulatorSnowflake, cfg.Containers, false, path)
	require.NoError(t, err)
	assert.Equal(t, config.EmulatorSnowflake, containers[0].Type)
	assert.Contains(t, buf.String(), "Switched configured emulator to Snowflake")
}

// TestApplyEmulatorType_SwitchProceedsWhenRuntimeUnreachable pins the fail-open
// behavior: when the conflict scan itself errors (e.g. Docker unreachable), the
// switch must still proceed rather than being blocked by an optimization that
// couldn't be verified — the real connectivity problem surfaces later, from the
// start attempt itself.
func TestApplyEmulatorType_SwitchProceedsWhenRuntimeUnreachable(t *testing.T) {
	path := loadTempConfig(t, "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n")
	cfg, err := config.Get()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, assert.AnError)

	var buf bytes.Buffer
	containers, err := ApplyEmulatorType(context.Background(), mockRT, output.NewPlainSink(&buf), config.EmulatorAzure, cfg.Containers, false, path)
	require.NoError(t, err)
	assert.Equal(t, config.EmulatorAzure, containers[0].Type)
}

// TestApplyEmulatorType_RefusesFirstRunWhenDifferentEmulatorRunning covers the
// same guard on the first-run path (no config file yet): if some other
// emulator is already running on the default port, the fresh config must not
// be created recording a type that would immediately conflict with it.
func TestApplyEmulatorType_RefusesFirstRunWhenDifferentEmulatorRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoFileExists(t, path)

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), config.KnownImageRepos(), "4566/tcp").
		Return(&runtime.RunningContainer{Name: "localstack-aws", Image: "localstack/localstack-pro:latest", BoundPort: "4566"}, nil)

	var buf bytes.Buffer
	_, err := ApplyEmulatorType(context.Background(), mockRT, output.NewPlainSink(&buf), config.EmulatorSnowflake, nil, true, path)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
	assert.Contains(t, buf.String(), "LocalStack AWS Emulator is running on port 4566")
	assert.NoFileExists(t, path)
}
