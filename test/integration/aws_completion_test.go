package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/test/integration/env"
)

// writeFakeAWSCompleter creates a stand-in for the AWS CLI's aws_completer:
// it echoes the COMP_LINE it was handed and then a fixed candidate list.
// Returns the directory containing it, to prepend to PATH. The echoed line is
// bracketed both so assertions can tell it apart from a candidate and so a
// significant trailing space survives the whitespace trim every candidate gets.
func writeFakeAWSCompleter(t *testing.T) string {
	t.Helper()
	return writeFakeTool(t, "aws_completer", fakeToolConfig{
		Stdout: []string{"compline=[{env:COMP_LINE}]", "ls", "list-buckets"},
	})
}

// TestAWSCompletionDelegatesToAWSCompleter covers DEVX-846: `lstk aws <TAB>`
// must produce the aws CLI's own candidates. Cobra's __complete is the entry
// point every generated completion script calls, so exercising it here covers
// bash, zsh, fish and powershell at once.
func TestAWSCompletionDelegatesToAWSCompleter(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWSCompleter(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "__complete", "aws", "s3", "l")
	require.NoError(t, err, "stderr: %s", stderr)

	// The completer strips the first word of COMP_LINE and resolves the rest
	// against the aws command tree, so lstk's own name must not be in it.
	require.Contains(t, stdout, "compline=[aws s3 l]")
	require.NotContains(t, stdout, "compline=[lstk")

	completions := strings.Fields(stdout)
	require.Contains(t, completions, "ls")
	require.Contains(t, completions, "list-buckets")
}

// TestAWSCompletionAppendsTrailingSpaceForNewWord verifies the cursor position
// is conveyed: with the cursor after a space the completer must see a trailing
// space, or it would offer completions for the previous word instead.
func TestAWSCompletionAppendsTrailingSpaceForNewWord(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWSCompleter(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "__complete", "aws", "s3", "")
	require.NoError(t, err, "stderr: %s", stderr)

	require.Contains(t, stdout, "compline=[aws s3 ]")
}

// TestAWSCompletionStripsGlobalFlags keeps lstk's own persistent flags out of
// the line handed to the aws completer, mirroring what stripGlobalFlags does
// for the exec path.
func TestAWSCompletionStripsGlobalFlags(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWSCompleter(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"__complete", "aws", "--non-interactive", "s3", "l")
	require.NoError(t, err, "stderr: %s", stderr)

	require.Contains(t, stdout, "compline=[aws s3 l]")
}

// TestAWSCompletionNeedsNoDockerOrEmulator guards the property that makes this
// usable at all: Cobra does not run PreRunE on the __complete path, so pressing
// Tab must not load config, contact Docker, or resolve an endpoint.
func TestAWSCompletionNeedsNoDockerOrEmulator(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWSCompleter(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		WithHome(t.TempDir()).
		With(env.Key("DOCKER_HOST"), "tcp://localhost:1")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "__complete", "aws", "s3", "l")
	require.NoError(t, err, "stderr: %s", stderr)

	require.Contains(t, stdout, "ls")
	require.NotContains(t, stdout, "Docker is not available")
}

// TestAWSCompletionDegradesWhenCompleterMissing verifies a machine without
// aws_completer still gets a clean, silent no-op: anything printed on this path
// would be read by the shell as a candidate.
func TestAWSCompletionDegradesWhenCompleterMissing(t *testing.T) {
	t.Parallel()

	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "__complete", "aws", "s3", "l")
	require.NoError(t, err, "stderr: %s", stderr)

	// ShellCompDirectiveDefault (0) — hand the word back to the shell's own
	// file completion rather than offering nothing at all.
	require.Equal(t, ":0", strings.TrimSpace(stdout))
}

// TestBashCompletionForAWSSubcommand drives the real generated bash script the
// way a Tab press does, proving the delegation survives the completion script
// and not just a direct __complete call.
func TestBashCompletionForAWSSubcommand(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWSCompleter(t)
	driver := completeInDriver(`lstk aws s3 l`, 3, "lstk aws s3 l")

	stdout, stderr, err := runBashCompletionDriver(t, driver, fakeDir)
	require.NoError(t, err, "completion attempt failed\nstdout: %s\nstderr: %s", stdout, stderr)
	require.NotContains(t, stderr, "command not found")

	completions := strings.Fields(stdout)
	require.Contains(t, completions, "ls")
	require.Contains(t, completions, "list-buckets")
}
