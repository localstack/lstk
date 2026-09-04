package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundleMissing(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		goos  string
		want  bool
	}{
		{name: "nothing beside lstk", goos: "linux", want: true},
		{name: "binary present", files: []string{"bundled-extensions"}, goos: "linux"},
		{name: "toml present", files: []string{"lstk-extensions.toml"}, goos: "linux"},
		{name: "both present", files: []string{"bundled-extensions", "lstk-extensions.toml"}, goos: "linux"},
		{name: "windows binary present", files: []string{"bundled-extensions.exe"}, goos: "windows"},
		{name: "unix binary name does not count on windows", files: []string{"bundled-extensions"}, goos: "windows", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644))
			}
			assert.Equal(t, tc.want, bundleMissing(dir, tc.goos))
		})
	}
}

func TestDetectMissingBundleForInstall(t *testing.T) {
	dir := t.TempDir()
	info := InstallInfo{Method: InstallHomebrew, ResolvedPath: filepath.Join(dir, "lstk")}

	mb, ok := detectMissingBundle(info, "linux")
	require.True(t, ok)
	assert.Equal(t, dir, mb.Dir)
	assert.Equal(t, "brew reinstall --cask localstack/tap/lstk", mb.Reinstall)
	assert.Contains(t, mb.Summary(), dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk-extensions.toml"), nil, 0o644))
	_, ok = detectMissingBundle(info, "linux")
	assert.False(t, ok)
}

func TestReinstallCommand(t *testing.T) {
	assert.Equal(t, "brew reinstall --cask localstack/tap/lstk", ReinstallCommand(InstallHomebrew))
	assert.Equal(t, "npm install -g @localstack/lstk", ReinstallCommand(InstallNPM))
	assert.Contains(t, ReinstallCommand(InstallBinary), "https://github.com/localstack/lstk/releases/latest")
}
