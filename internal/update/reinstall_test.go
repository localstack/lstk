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

// Only a plain binary install can be left without its bundle: Homebrew and npm
// replace the whole package.
func TestDetectMissingBundleByInstallMethod(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "lstk")

	mb, ok := detectMissingBundle(InstallInfo{Method: InstallBinary, ResolvedPath: exe}, "linux")
	require.True(t, ok)
	assert.Equal(t, dir, mb.Dir)
	assert.Contains(t, mb.Reinstall, "https://github.com/localstack/lstk/releases/latest")
	assert.Contains(t, mb.Summary(), dir)

	for _, m := range []InstallMethod{InstallHomebrew, InstallNPM} {
		_, ok := detectMissingBundle(InstallInfo{Method: m, ResolvedPath: exe}, "linux")
		assert.False(t, ok, m.String())
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk-extensions.toml"), nil, 0o644))
	_, ok = detectMissingBundle(InstallInfo{Method: InstallBinary, ResolvedPath: exe}, "linux")
	assert.False(t, ok)
}
