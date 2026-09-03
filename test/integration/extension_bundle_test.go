package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// Pins: both bundled commands with their descriptions and the PATH one
	// name-only. Only the Extensions section is snapshotted; the rest of the
	// help text belongs to commands and flags this test says nothing about.
	snap.Match(t, helpSection(t, stdout, "Extensions:"))
	// The bundle binary itself is not a command, and help never executes it.
	require.NotContains(t, stdout, "bundled-extensions")
	require.NotContains(t, stdout, "ARGS=")
}

// helpSection returns one section of `lstk --help`: the header line and the
// indented entries under it, up to the blank line that ends it. Tests pin a
// section rather than the whole help text, so that adding a command or a flag
// elsewhere in lstk does not rewrite an extensions snapshot.
func helpSection(t *testing.T, help, header string) string {
	t.Helper()
	lines := strings.Split(strings.ReplaceAll(help, "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if line == header {
			start = i
			break
		}
	}
	require.NotEqual(t, -1, start, "no %q section in help output:\n%s", header, help)
	end := start + 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
		end++
	}
	return strings.Join(lines[start:end], "\n")
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

// A release ships the bundle binary and the descriptions file and nothing else,
// but a user can still drop an lstk-<name> link to the binary next to it, and
// installs made before aliases were dropped will have some. lstk must be
// indifferent to them: it takes its command list from the descriptions file and
// dispatches by argv[0], so such a link must not add a second help entry or
// change what runs.
func TestBundledAliasSymlinksAreInertForLstk(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink needs Developer Mode or elevation on Windows")
	}
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	bundlePath := installMultiCallBundle(t, bundleDir, "doctor = \"Check the local setup\"\ndeploy = \"Deploy to LocalStack\"\n")
	for _, name := range []string{"doctor", "deploy"} {
		require.NoError(t, os.Symlink("bundled-extensions", filepath.Join(bundleDir, "lstk-"+name)))
	}

	tmpHome := t.TempDir()
	environ := envWithPath(tmpHome, t.TempDir())

	// Each command appears exactly once, with its description — not twice, and
	// not as a name-only row shadowing the described one.
	stdout, stderr, err := runBinary(t, t.TempDir(), environ, lstkBin, "--help")
	require.NoError(t, err, stderr)
	require.Equal(t, 1, strings.Count(stdout, "doctor      Check the local setup"))
	require.Equal(t, 1, strings.Count(stdout, "deploy      Deploy to LocalStack"))

	// Dispatch still goes through the bundle under the right argv[0].
	stdout, stderr, err = runBinary(t, t.TempDir(), environ, lstkBin, "doctor", "argv0")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "ARGV0=lstk-doctor")
	resolved, err := filepath.EvalSymlinks(bundlePath)
	require.NoError(t, err)
	require.Contains(t, stdout, "SELF="+resolved)
}
