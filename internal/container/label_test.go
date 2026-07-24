package container

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeActivatedLicense(t *testing.T, volumeDir, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(volumeDir, "cache"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(volumeDir, "cache", "license.json"), []byte(content), 0600))
}

// The nil PlatformAPI client in these tests doubles as an assertion: the
// self-validating path must never call the license API (it would panic).
func TestResolveEmulatorLabelReadsActivatedLicenseForSelfValidating(t *testing.T) {
	volumeDir := t.TempDir()
	writeActivatedLicense(t, volumeDir, `{"license_type":"enterprise","license_status":"ACTIVE"}`)

	containers := []config.ContainerConfig{{Type: config.EmulatorAzure, Tag: "latest", Volume: volumeDir}}
	label, ok := ResolveEmulatorLabel(context.Background(), nil, containers, "token", "", log.Nop())

	assert.Equal(t, "LocalStack Enterprise", label)
	assert.True(t, ok, "label resolved from an activated license should be cached")
}

func TestResolveEmulatorLabelReadsActivatedLicenseForPinnedTag(t *testing.T) {
	volumeDir := t.TempDir()
	writeActivatedLicense(t, volumeDir, `{"license_type":"ultimate"}`)

	containers := []config.ContainerConfig{{Type: config.EmulatorSnowflake, Tag: "1.2.3", Volume: volumeDir}}
	label, ok := ResolveEmulatorLabel(context.Background(), nil, containers, "token", "", log.Nop())

	assert.Equal(t, "LocalStack Ultimate", label)
	assert.True(t, ok)
}

func TestResolveEmulatorLabelFallsBackWhenActivatedLicenseMissing(t *testing.T) {
	containers := []config.ContainerConfig{{Type: config.EmulatorAzure, Tag: "latest", Volume: t.TempDir()}}
	label, ok := ResolveEmulatorLabel(context.Background(), nil, containers, "token", "", log.Nop())

	assert.Equal(t, "LocalStack", label)
	assert.False(t, ok, "fallback label must not be cached")
}

func TestResolveEmulatorLabelFallsBackWhenActivatedLicenseInvalid(t *testing.T) {
	for name, content := range map[string]string{
		"corrupt JSON":    `{not json`,
		"no license_type": `{"license_status":"ACTIVE"}`,
	} {
		t.Run(name, func(t *testing.T) {
			volumeDir := t.TempDir()
			writeActivatedLicense(t, volumeDir, content)

			containers := []config.ContainerConfig{{Type: config.EmulatorAzure, Tag: "latest", Volume: volumeDir}}
			label, ok := ResolveEmulatorLabel(context.Background(), nil, containers, "token", "", log.Nop())

			assert.Equal(t, "LocalStack", label)
			assert.False(t, ok)
		})
	}
}
