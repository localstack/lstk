package update

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyPathExternalManagers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		wantMethod  InstallMethod
		wantManager string
	}{
		{
			name:        "nix store",
			path:        "/nix/store/9zk1abcd-lstk-0.5.0/bin/lstk",
			wantMethod:  InstallExternal,
			wantManager: "nix",
		},
		{
			name:        "guix store",
			path:        "/gnu/store/abcd1234-lstk-0.5.0/bin/lstk",
			wantMethod:  InstallExternal,
			wantManager: "guix",
		},
		{
			name:        "mise install",
			path:        "/Users/joe/.local/share/mise/installs/github-localstack-lstk/latest/lstk",
			wantMethod:  InstallExternal,
			wantManager: "mise",
		},
		{
			name:        "mise shim",
			path:        "/Users/joe/.local/share/mise/shims/lstk",
			wantMethod:  InstallExternal,
			wantManager: "mise",
		},
		{
			name:        "legacy rtx install reports as mise",
			path:        "/Users/joe/.local/share/rtx/installs/lstk/0.5.0/lstk",
			wantMethod:  InstallExternal,
			wantManager: "mise",
		},
		{
			name:        "asdf via ASDF_DATA_DIR (no leading dot)",
			path:        "/home/user/.local/share/asdf/installs/lstk/0.5.0/bin/lstk",
			wantMethod:  InstallExternal,
			wantManager: "asdf",
		},
		{
			name:        "scoop shim (the path actually on PATH)",
			path:        "C:/Users/joe/scoop/shims/lstk.exe",
			wantMethod:  InstallExternal,
			wantManager: "scoop",
		},
		{
			name:        "chocolatey bin (the path actually on PATH)",
			path:        "C:/ProgramData/chocolatey/bin/lstk.exe",
			wantMethod:  InstallExternal,
			wantManager: "chocolatey",
		},
		{
			name:        "asdf install",
			path:        "/home/user/.asdf/installs/lstk/0.5.0/bin/lstk",
			wantMethod:  InstallExternal,
			wantManager: "asdf",
		},
		{
			name:        "asdf shim",
			path:        "/home/user/.asdf/shims/lstk",
			wantMethod:  InstallExternal,
			wantManager: "asdf",
		},
		{
			name:        "scoop",
			path:        "C:/Users/joe/scoop/apps/lstk/current/lstk.exe",
			wantMethod:  InstallExternal,
			wantManager: "scoop",
		},
		{
			name:        "chocolatey",
			path:        "C:/ProgramData/chocolatey/lib/lstk/tools/lstk.exe",
			wantMethod:  InstallExternal,
			wantManager: "chocolatey",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			method, manager := classifyPath(tt.path)
			assert.Equal(t, tt.wantMethod, method)
			assert.Equal(t, tt.wantManager, manager)
		})
	}
}

// The npm and Homebrew install methods can update themselves correctly even
// when their interpreter or prefix was provisioned by a tool manager, so their
// markers must win over the tool-manager markers regardless of which appears
// first in the path.
func TestClassifyPathSelfUpdatableMethodsWinOverToolManagers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want InstallMethod
	}{
		{
			name: "npm under a mise-managed node",
			path: "/Users/someone/.local/share/mise/installs/node/24.8.0/lib/node_modules/@localstack/lstk_darwin_arm64/lstk",
			want: InstallNPM,
		},
		{
			name: "npm under an asdf-managed node",
			path: "/Users/geo/.asdf/installs/nodejs/22.12.0/lib/node_modules/@localstack/lstk_darwin_arm64/lstk",
			want: InstallNPM,
		},
		{
			name: "homebrew cask under a scoop-shaped path",
			path: "/opt/homebrew/Caskroom/lstk/0.3.0/lstk",
			want: InstallHomebrew,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			method, manager := classifyPath(tt.path)
			assert.Equal(t, tt.want, method)
			assert.Empty(t, manager, "only an externally-managed install names a manager")
		})
	}
}

func TestClassifyPathOrdinaryInstallsNameNoManager(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/usr/local/bin/lstk", "/home/user/bin/lstk", "/home/user/Projects/lstk/bin/lstk"} {
		method, manager := classifyPath(path)
		assert.Equal(t, InstallBinary, method, path)
		assert.Empty(t, manager, path)
	}
}

func TestInstallDirWritable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writable, err := installDirWritable(filepath.Join(dir, "lstk"))
	require.NoError(t, err)
	assert.True(t, writable)
}

func TestInstallDirNotWritable(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("POSIX directory permissions do not port to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	t.Parallel()

	dir := t.TempDir()
	readOnly := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(readOnly, 0500))

	writable, err := installDirWritable(filepath.Join(readOnly, "lstk"))
	require.NoError(t, err)
	assert.False(t, writable)
}

func TestInstallDirWritableLeavesNoProbeFileBehind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := installDirWritable(filepath.Join(dir, "lstk"))
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the probe must not leave a file in the install directory")
}

// The JSON envelope's "method" field reports how the update was performed, and
// its documented values are homebrew/npm/binary. A --force update on an
// externally-managed install performs a binary replacement, so it must report
// "binary" rather than leaking the new install-method name into that enum.
func TestAppliedMethodName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "homebrew", appliedMethodName(InstallHomebrew))
	assert.Equal(t, "npm", appliedMethodName(InstallNPM))
	assert.Equal(t, "binary", appliedMethodName(InstallBinary))
	assert.Equal(t, "binary", appliedMethodName(InstallExternal))
}

func TestBlockSelfUpdate(t *testing.T) {
	t.Parallel()

	t.Run("external install is blocked and names the manager", func(t *testing.T) {
		t.Parallel()
		blocker := blockSelfUpdate(InstallInfo{
			Method:       InstallExternal,
			Manager:      "mise",
			ResolvedPath: "/home/u/.local/share/mise/installs/lstk/latest/lstk",
		})
		require.NotNil(t, blocker)
		assert.Equal(t, "mise", blocker.Manager)
		assert.Contains(t, blocker.title(), "mise")
	})

	t.Run("homebrew is never blocked", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, blockSelfUpdate(InstallInfo{Method: InstallHomebrew, ResolvedPath: "/opt/homebrew/Caskroom/lstk/1/lstk"}))
	})

	t.Run("npm is never blocked", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, blockSelfUpdate(InstallInfo{Method: InstallNPM, ResolvedPath: "/usr/local/lib/node_modules/x/lstk"}))
	})

	t.Run("writable binary install proceeds", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, blockSelfUpdate(InstallInfo{Method: InstallBinary, ResolvedPath: filepath.Join(t.TempDir(), "lstk")}))
	})

	t.Run("read-only binary install is blocked and names the directory", func(t *testing.T) {
		if goruntime.GOOS == "windows" {
			t.Skip("POSIX directory permissions do not port to Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions")
		}
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "ro")
		require.NoError(t, os.Mkdir(dir, 0500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

		blocker := blockSelfUpdate(InstallInfo{Method: InstallBinary, ResolvedPath: filepath.Join(dir, "lstk")})
		require.NotNil(t, blocker)
		assert.Empty(t, blocker.Manager)
		assert.Contains(t, blocker.action().Label, dir)
	})

}

// os.Executable() failed, so there is no install directory. filepath.Dir("") is
// ".", so an unguarded probe runs against the working directory: from a
// read-only cwd it reports the install as unwritable and refuses the update,
// naming a directory that has nothing to do with where lstk lives.
//
// Not parallel: t.Chdir cannot be used from a parallel test.
func TestBlockSelfUpdateUnknownPathIgnoresWorkingDirectory(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("POSIX directory permissions do not port to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	readOnly := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.Mkdir(readOnly, 0500))
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0700) })
	t.Chdir(readOnly)

	assert.Nil(t, blockSelfUpdate(InstallInfo{Method: InstallBinary, ResolvedPath: ""}))
}
