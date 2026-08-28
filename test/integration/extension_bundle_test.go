package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/snap"
	"github.com/stretchr/testify/require"
)

// LocalStack's bundled extensions ship as ONE multi-call binary,
// `bundled-extensions`, next to lstk; the descriptions file lists the commands
// it provides, and lstk execs it with argv[0] set to `lstk-<name>` so the binary
// knows which extension to be. These tests install the reference extension
// under that name and assert the user-visible contract: `lstk <name>` runs it
// under the right argv[0], help lists every described command, and a command
// the file does not describe is not handed to the bundle.

// installMultiCallBundle places the reference extension in dir as the multi-call
// bundled binary and writes a descriptions file naming the given commands.
func installMultiCallBundle(t *testing.T, dir string, descriptions string) string {
	t.Helper()
	path := filepath.Join(dir, execName("bundled-extensions"))
	copyExecutable(t, referenceExtensionBinary(t), path)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk-extensions.toml"), []byte(descriptions), 0o644))
	return path
}

func TestBundledMultiCallDispatchesWithArgv0(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	installMultiCallBundle(t, bundleDir, "doctor = \"Check the local setup\"\ndeploy = \"Deploy to LocalStack\"\n")

	tmpHome := t.TempDir()
	environ := envWithPath(tmpHome, t.TempDir())

	stdout, stderr, err := runBinary(t, t.TempDir(), environ, lstkBin, "doctor", "argv0")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "ARGS=[argv0]")
	require.Contains(t, stdout, "ARGV0=lstk-doctor", "the bundle must be told which extension to be via argv[0]")

	// The same binary, a different name.
	stdout, stderr, err = runBinary(t, t.TempDir(), environ, lstkBin, "deploy", "argv0")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "ARGV0=lstk-deploy")
}

func TestBundledMultiCallRunsTheBundledBinary(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	bundlePath := installMultiCallBundle(t, bundleDir, "doctor = \"Check the local setup\"\n")

	// A same-named lstk-doctor on PATH must lose to the bundle.
	extDir := t.TempDir()
	installExtension(t, extDir, "doctor")

	tmpHome := t.TempDir()
	stdout, stderr, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, extDir), lstkBin, "doctor")
	require.NoError(t, err, stderr)
	resolvedBundle, err := filepath.EvalSymlinks(bundlePath)
	require.NoError(t, err)
	require.Contains(t, stdout, "SELF="+resolvedBundle, "expected the bundled binary to run, not the PATH one")
}

func TestBundledMultiCallHelpListsDescribedCommands(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	installMultiCallBundle(t, bundleDir, "doctor = \"Check the local setup\"\ndeploy = \"Deploy to LocalStack\"\n")

	extDir := t.TempDir()
	installExtension(t, extDir, "hello") // PATH-only, name-only in help

	tmpHome := t.TempDir()
	stdout, stderr, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, extDir), lstkBin, "--help")
	require.NoError(t, err, stderr)
	// Pins: both bundled commands with their descriptions, the PATH one
	// name-only, no `bundled-extensions` or `extensions` phantom entries, and
	// no ARGS= line (help never executes anything).
	snap.Match(t, stdout)
}

func TestBundledMultiCallUndescribedCommandIsUnknown(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	installMultiCallBundle(t, bundleDir, "doctor = \"Check the local setup\"\n")

	tmpHome := t.TempDir()
	// `other` is not in the descriptions file, so it must not reach the bundle;
	// with nothing on PATH either, it is an unknown command.
	stdout, stderr, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, t.TempDir()), lstkBin, "other")
	requireExitCode(t, 1, err)
	require.NotContains(t, stdout, "ARGS=", "the bundle must not have been executed")
	require.Contains(t, stderr, `unknown command "other"`)
}

func TestBundledMultiCallBinaryWithoutDescriptionsIsAnError(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	// The binary shipped but its descriptions file did not: a broken install.
	// That must surface as an error, not as "unknown command".
	copyExecutable(t, referenceExtensionBinary(t), filepath.Join(bundleDir, execName("bundled-extensions")))

	tmpHome := t.TempDir()
	stdout, stderr, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, t.TempDir()), lstkBin, "doctor")
	require.Error(t, err)
	require.NotContains(t, stdout, "ARGS=")
	require.NotContains(t, stderr, "unknown command")
	require.Contains(t, stderr, "lstk-extensions.toml")
}

func TestBundledMultiCallContextConveyed(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	installMultiCallBundle(t, bundleDir, "doctor = \"Check the local setup\"\n")

	tmpHome := t.TempDir()
	environ := append(envWithPath(tmpHome, t.TempDir()), "DOCKER_HOST=tcp://127.0.0.1:1")
	stdout, stderr, err := runBinary(t, t.TempDir(), environ, lstkBin, "--non-interactive", "doctor", "--foo")
	require.NoError(t, err, stderr)
	// Same contract as a standalone extension: args forwarded, lstk's own flag
	// consumed and conveyed, API version and context present.
	require.Contains(t, stdout, "ARGS=[--foo]")
	require.Contains(t, stdout, "NON_INTERACTIVE=true")
	require.Contains(t, stdout, "API_VERSION=1")
}
