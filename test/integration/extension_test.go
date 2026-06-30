package integration_test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
)

// The extension mechanism resolves and execs `lstk-<name>` executables for
// unknown commands, conveying runtime context via LSTK_EXT_* env vars. These
// tests use the in-tree reference extension (test-samples/extensions/lstk-ref) built under
// various names and placed on PATH or in the binary's bundled directory.

var (
	refExtOnce sync.Once
	refExtPath string
	refExtErr  error
)

// referenceExtensionBinary builds test-samples/extensions/lstk-ref once and returns the path to
// the compiled binary. The same binary backs every extension name in the tests
// (it just echoes its decoded LSTK_EXT_CONTEXT, forwards args, and can exit with a
// chosen code or perform a stubbed self-authorization). The reference extension
// lives inside this `test/integration` module (its own go.mod), so it is built
// from the module root, not the repo root.
func referenceExtensionBinary(t *testing.T) string {
	t.Helper()
	refExtOnce.Do(func() {
		// The test runs with its working directory at the integration module root.
		moduleRoot, err := filepath.Abs(".")
		if err != nil {
			refExtErr = err
			return
		}
		dir, err := os.MkdirTemp("", "lstk-ref-build-*")
		if err != nil {
			refExtErr = err
			return
		}
		out := filepath.Join(dir, "lstk-ref-bin")
		cmd := exec.Command("go", "build", "-o", out, "./test-samples/extensions/lstk-ref")
		cmd.Dir = moduleRoot
		if b, err := cmd.CombinedOutput(); err != nil {
			refExtErr = fmt.Errorf("build reference extension: %w: %s", err, b)
			return
		}
		refExtPath = out
	})
	require.NoError(t, refExtErr)
	return refExtPath
}

func execName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	require.NoError(t, err)
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	require.NoError(t, err)
	_, err = io.Copy(out, in)
	require.NoError(t, err)
	require.NoError(t, out.Close())
}

// installExtension places the reference extension under the name `lstk-<name>`
// in dir and returns the directory.
func installExtension(t *testing.T, dir, name string) {
	t.Helper()
	copyExecutable(t, referenceExtensionBinary(t), filepath.Join(dir, execName("lstk-"+name)))
}

// installLstkBundle copies the built lstk binary into dir so that dir becomes
// lstk's bundled-extensions directory (lstk derives it from its own
// symlink-resolved executable location). Returns the path to the copied binary.
func installLstkBundle(t *testing.T, dir string) string {
	t.Helper()
	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)
	dst := filepath.Join(dir, execName("lstk"))
	copyExecutable(t, binPath, dst)
	return dst
}

// envWithPath returns a test environment with extDir prepended to PATH and an
// isolated HOME, so extensions placed in extDir resolve and no real user state
// is touched. Extra DOCKER_HOST etc. can be appended by the caller.
func envWithPath(tmpHome, extDir string) []string {
	e := testEnvWithHome(tmpHome, "")
	e = append(e, "PATH="+extDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return e
}

// runBinary runs an arbitrary lstk binary (not necessarily ../../bin/lstk) with
// the given env, returning trimmed stdout, stderr, and the run error.
func runBinary(t *testing.T, dir string, environ []string, binPath string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.CommandContext(testContext(t), binPath, args...)
	cmd.Dir = dir
	cmd.Env = environ
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func TestExtensionBuiltinTakesPrecedence(t *testing.T) {
	t.Parallel()
	extDir := t.TempDir()
	// An lstk-config on PATH must NOT shadow the built-in `config` command.
	installExtension(t, extDir, "config")

	tmpHome := t.TempDir()
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), envWithPath(tmpHome, extDir), "config", "path")
	require.NoError(t, err, stderr)
	// The built-in prints a config path; the extension would have printed ARGS=.
	require.Contains(t, stdout, "config.toml")
	require.NotContains(t, stdout, "ARGS=")
}

func TestExtensionUnknownCommandDispatches(t *testing.T) {
	t.Parallel()
	extDir := t.TempDir()
	installExtension(t, extDir, "hello")

	tmpHome := t.TempDir()
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), envWithPath(tmpHome, extDir), "hello", "world")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "ARGS=[world]")
	require.Contains(t, stdout, "API_VERSION=1")
	require.Contains(t, stdout, "CONFIG_DIR=")
}

func TestExtensionUnknownCommandNoExtensionErrors(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	// Empty extension dir so `nope` resolves nowhere. The error event renders
	// through the plain sink (stdout), consistent with other lstk commands.
	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), envWithPath(tmpHome, t.TempDir()), "nope")
	requireExitCode(t, 1, err)
	require.Contains(t, stdout, "unknown command")
}

func TestExtensionExitCodePropagates(t *testing.T) {
	t.Parallel()
	extDir := t.TempDir()
	installExtension(t, extDir, "hello")

	tmpHome := t.TempDir()
	_, _, err := runLstk(t, testContext(t), t.TempDir(), envWithPath(tmpHome, extDir), "hello", "exit", "7")
	requireExitCode(t, 7, err)
}

func TestExtensionGlobalFlagConveyedNotForwarded(t *testing.T) {
	t.Parallel()
	extDir := t.TempDir()
	installExtension(t, extDir, "hello")

	tmpHome := t.TempDir()
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), envWithPath(tmpHome, extDir), "--non-interactive", "hello", "--foo")
	require.NoError(t, err, stderr)
	// --non-interactive is consumed by lstk and conveyed via env, not forwarded.
	require.Contains(t, stdout, "ARGS=[--foo]")
	require.Contains(t, stdout, "NON_INTERACTIVE=true")
}

func TestExtensionAuthTokenConveyedWhenAuthed(t *testing.T) {
	t.Parallel()
	extDir := t.TempDir()
	installExtension(t, extDir, "ref")

	tmpHome := t.TempDir()
	environ := envWithPath(tmpHome, extDir)
	environ = append(environ, string(env.AuthToken)+"=tok-abc-123")
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "ref")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "AUTH_TOKEN=tok-abc-123")
}

func TestExtensionEndpointOmittedWhenNoRuntime(t *testing.T) {
	t.Parallel()
	extDir := t.TempDir()
	installExtension(t, extDir, "ref")

	tmpHome := t.TempDir()
	environ := envWithPath(tmpHome, extDir)
	// Point DOCKER_HOST at a closed port so the runtime is unavailable and the
	// emulator context is deterministically omitted.
	environ = append(environ, "DOCKER_HOST=tcp://127.0.0.1:1")
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "ref")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "EMULATOR_COUNT=0")
	require.NotContains(t, stdout, "EMULATOR=")
	// The extension still runs and still receives the always-present variables.
	require.Contains(t, stdout, "API_VERSION=1")
}

func TestExtensionSelfAuthorizationRefusesWithoutToken(t *testing.T) {
	t.Parallel()
	extDir := t.TempDir()
	installExtension(t, extDir, "deploy")

	tmpHome := t.TempDir()
	environ := envWithPath(tmpHome, extDir)
	environ = append(environ, "DOCKER_HOST=tcp://127.0.0.1:1")
	// No auth token: the extension's stubbed self-authorization refuses (exit 13).
	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "deploy", "auth")
	requireExitCode(t, 13, err)
	require.Contains(t, stderr, "not authorized")
}

func TestExtensionEndpointConveyedWhenEmulatorRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	extDir := t.TempDir()
	installExtension(t, extDir, "ref")

	tmpHome := t.TempDir()
	environ := envWithPath(tmpHome, extDir)
	environ = append(environ, string(env.AuthToken)+"=tok-xyz")
	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), environ, "ref")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "EMULATOR=aws http")
	require.Contains(t, stdout, "AUTH_TOKEN=tok-xyz")
}

func TestExtensionHelpListsBundledWithDescriptionAndPathNameOnly(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	installExtension(t, bundleDir, "deploy") // bundled
	require.NoError(t, os.WriteFile(
		filepath.Join(bundleDir, "lstk-extensions.toml"),
		[]byte("deploy = \"Deploy your application to LocalStack\"\n"), 0o644))

	extDir := t.TempDir()
	installExtension(t, extDir, "hello") // PATH-only

	tmpHome := t.TempDir()
	stdout, stderr, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, extDir), lstkBin, "--help")
	require.NoError(t, err, stderr)

	require.Contains(t, stdout, "Extensions:")
	require.Contains(t, stdout, "deploy")
	require.Contains(t, stdout, "Deploy your application to LocalStack")
	require.Contains(t, stdout, "hello")
	// Help must not execute any extension.
	require.NotContains(t, stdout, "ARGS=")
}

// TestExtensionHelpDescriptionColumnAlignsWithCommands guards the bug where the
// Extensions section computed its own padding (local-max name width + 2 spaces)
// and rendered descriptions in a different column than the Commands/Tools
// sections (Cobra's NamePadding + 1 space). The fix has the Extensions section
// reuse the root command's NamePadding, so both columns line up.
func TestExtensionHelpDescriptionColumnAlignsWithCommands(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	installExtension(t, bundleDir, "deploy")
	require.NoError(t, os.WriteFile(
		filepath.Join(bundleDir, "lstk-extensions.toml"),
		[]byte("deploy = \"Deploy your application to LocalStack\"\n"), 0o644))

	tmpHome := t.TempDir()
	stdout, stderr, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, t.TempDir()), lstkBin, "--help")
	require.NoError(t, err, stderr)

	lines := strings.Split(stdout, "\n")
	// The built-in `aws` command (in the Tools group) sets the reference column.
	cmdCol := descriptionColumn(t, lines, "aws", "Run AWS CLI commands against LocalStack")
	extCol := descriptionColumn(t, lines, "deploy", "Deploy your application to LocalStack")
	require.Equal(t, cmdCol, extCol,
		"extension description column (%d) must align with command description column (%d)", extCol, cmdCol)
}

// descriptionColumn returns the byte index at which desc begins on the help line
// for the command/extension named name (a "  <name> ... <desc>" row). It fails
// the test if no such line is found.
func descriptionColumn(t *testing.T, lines []string, name, desc string) int {
	t.Helper()
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  "+name+" ") && strings.Contains(ln, desc) {
			return strings.Index(ln, desc)
		}
	}
	t.Fatalf("no help line for %q with description %q in:\n%s", name, desc, strings.Join(lines, "\n"))
	return -1
}

func TestExtensionHelpMissingDescriptionsFileDegrades(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	installExtension(t, bundleDir, "deploy") // bundled, but no descriptions file

	tmpHome := t.TempDir()
	stdout, stderr, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, t.TempDir()), lstkBin, "--help")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "Extensions:")
	require.Contains(t, stdout, "deploy")
}

func TestExtensionResolvableViaSymlinkedLstk(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink shim test is Unix-only")
	}
	// Real install dir holds lstk + its bundled extension; lstk is invoked via a
	// symlink elsewhere (mimicking an npm .bin link or Homebrew shim). lstk must
	// resolve its real location to find the bundled sibling.
	realDir := t.TempDir()
	realLstk := installLstkBundle(t, realDir)
	installExtension(t, realDir, "deploy")

	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "lstk")
	require.NoError(t, os.Symlink(realLstk, link))

	tmpHome := t.TempDir()
	// Empty PATH extension dir: deploy can only come from the bundled location.
	stdout, stderr, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, t.TempDir()), link, "deploy", "ok")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "ARGS=[ok]")
}

func TestExtensionBundledPremiumSelfAuthorizes(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	installExtension(t, bundleDir, "deploy") // bundled "premium" extension

	tmpHome := t.TempDir()
	noRuntime := append(envWithPath(tmpHome, t.TempDir()), "DOCKER_HOST=tcp://127.0.0.1:1")

	// Unentitled (no token): lstk still dispatches to the bundled extension, which
	// performs its own authorization and refuses.
	_, _, err := runBinary(t, t.TempDir(), noRuntime, lstkBin, "deploy", "auth")
	requireExitCode(t, 13, err)

	// Authed: the bundled extension authorizes successfully.
	authed := append(noRuntime, string(env.AuthToken)+"=tok-premium")
	stdout, stderr, err := runBinary(t, t.TempDir(), authed, lstkBin, "deploy", "auth")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "authorized")
}

func TestExtensionBundledWinsOverPath(t *testing.T) {
	t.Parallel()
	bundleDir := t.TempDir()
	lstkBin := installLstkBundle(t, bundleDir)
	installExtension(t, bundleDir, "deploy") // bundled

	extDir := t.TempDir()
	installExtension(t, extDir, "deploy") // same name on PATH

	tmpHome := t.TempDir()
	// The reference extension echoes its own resolved executable path as SELF=,
	// so we can confirm the *bundled* copy ran, not the same-named PATH copy.
	stdout, stderr, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, extDir), lstkBin, "deploy", "world")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "ARGS=[world]")
	// lstk derives its bundled dir from the symlink-resolved executable path, so
	// compare SELF against the resolved bundle dir (TempDir may be a symlink,
	// e.g. /var → /private/var on macOS).
	resolvedBundle, err := filepath.EvalSymlinks(bundleDir)
	require.NoError(t, err)
	require.Contains(t, stdout, "SELF="+resolvedBundle, "expected the bundled extension to run, not the PATH one")

	// Help lists the de-duplicated name exactly once.
	helpOut, _, err := runBinary(t, t.TempDir(), envWithPath(tmpHome, extDir), lstkBin, "--help")
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(helpOut, "\n  deploy"))
}
