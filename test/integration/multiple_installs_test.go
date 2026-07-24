package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
)

// copyBinaryTo copies the built lstk binary into dir under the platform's
// executable name, creating a distinct install (not a symlink, so it does not
// deduplicate against the source).
func copyBinaryTo(t *testing.T, dir string) string {
	t.Helper()
	src, err := filepath.Abs(binaryPath())
	require.NoError(t, err)
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	name := "lstk"
	if runtime.GOOS == "windows" {
		name = "lstk.exe"
	}
	dst := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(dst, data, 0o755))
	return dst
}

func TestUpdateCheckWarnsOnMultipleInstallsOnPath(t *testing.T) {
	t.Parallel()
	dirA, dirB := t.TempDir(), t.TempDir()
	pathA := copyBinaryTo(t, dirA)
	pathB := copyBinaryTo(t, dirB)

	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.Path, dirA+string(os.PathListSeparator)+dirB)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "update", "--check")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "Multiple lstk installations found on PATH:")
	require.Contains(t, stdout, pathA)
	require.Contains(t, stdout, pathB)
}

func TestUpdateCheckDoesNotWarnOnSingleInstall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	copyBinaryTo(t, dir)

	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.Path, dir)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "update", "--check")
	require.NoError(t, err, stderr)
	require.NotContains(t, stdout, "Multiple lstk installations found")
}

func TestUpdateCheckDoesNotWarnOnSymlinkedAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks on Windows requires elevated privileges")
	}
	t.Parallel()
	dirA, dirB := t.TempDir(), t.TempDir()
	pathA := copyBinaryTo(t, dirA)
	require.NoError(t, os.Symlink(pathA, filepath.Join(dirB, "lstk")))

	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.Path, dirA+string(os.PathListSeparator)+dirB)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "update", "--check")
	require.NoError(t, err, stderr)
	require.NotContains(t, stdout, "Multiple lstk installations found")
}

func TestUpdateCheckDoesNotWarnOnAsdfShimAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("asdf shell shims are Unix-specific")
	}
	t.Parallel()
	asdfDataDir := t.TempDir()
	installBin := filepath.Join(asdfDataDir, "installs", "nodejs", "22.22.0", "bin")
	npmPackage := filepath.Join(asdfDataDir, "installs", "nodejs", "22.22.0", "lib", "node_modules", "@localstack", "lstk")
	shimsDir := filepath.Join(asdfDataDir, "shims")
	for _, dir := range []string{installBin, npmPackage, shimsDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	npmBinary := copyBinaryTo(t, npmPackage)
	require.NoError(t, os.Symlink(npmBinary, filepath.Join(installBin, "lstk")))
	shim := "#!/usr/bin/env bash\n# asdf-plugin: nodejs 22.22.0\nexec asdf exec \"lstk\" \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(shimsDir, "lstk"), []byte(shim), 0o755))

	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.Path, installBin+string(os.PathListSeparator)+shimsDir)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "update", "--check")
	require.NoError(t, err, stderr)
	require.NotContains(t, stdout, "Multiple lstk installations found")
}

func TestUpdateCheckJSONReportsMultipleInstallsWarning(t *testing.T) {
	t.Parallel()
	dirA, dirB := t.TempDir(), t.TempDir()
	copyBinaryTo(t, dirA)
	copyBinaryTo(t, dirB)

	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.Path, dirA+string(os.PathListSeparator)+dirB)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "update", "--check", "--json")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, `"MULTIPLE_INSTALLS"`)
}
