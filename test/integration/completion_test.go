package integration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/test/integration/env"
)

// stockBash returns the bash to test against, preferring /bin/bash (bash 3.2
// on macOS, the environment DEVX-950 was reported on) over whatever is first
// in PATH.
func stockBash(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bash completion not applicable on Windows")
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	return bashPath
}

// runBashCompletionDriver generates the bash completion script, writes it and
// the given driver script to disk, and runs the driver in a bare bash
// (--noprofile --norc, minimal env) so nothing from the developer's machine —
// in particular the bash-completion package — can leak in. That bare shell is
// exactly what DEVX-950 reproduces: stock macOS ships bash 3.2 with no
// bash-completion, so the generated script must be self-contained. The driver
// receives the completion script path as $1.
func runBashCompletionDriver(t *testing.T, driver string) (stdout, stderr string, err error) {
	t.Helper()
	bashPath := stockBash(t)

	tmpHome := t.TempDir()
	script, genStderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(tmpHome, ""), "completion", "bash")
	require.NoError(t, err, "lstk completion bash failed: %s", genStderr)

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "lstk-completion.bash")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o600))
	driverPath := filepath.Join(dir, "driver.bash")
	require.NoError(t, os.WriteFile(driverPath, []byte(driver), 0o600))

	binDir, err := filepath.Abs(filepath.Dir(binaryPath()))
	require.NoError(t, err)

	cmd := exec.CommandContext(testContext(t), bashPath, "--noprofile", "--norc", driverPath, scriptPath)
	cmd.Dir = dir
	// The driver invokes `lstk __complete ...`; force the file keyring so
	// that subprocess never probes the developer's system keyring.
	cmd.Env = []string{
		"HOME=" + tmpHome,
		"PATH=" + binDir + ":/usr/bin:/bin",
		fmt.Sprintf("%s=file", env.Keyring),
	}
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// completeInDriver simulates pressing Tab on the given command line state and
// prints the resulting COMPREPLY, one completion per line. compWords is a
// bash array literal (readline splits at COMP_WORDBREAKS characters, '=' and
// ':' included, so a typed '--flag=value' must be given as '--flag = value').
func completeInDriver(compWords string, cword int, line string) string {
	return fmt.Sprintf(`source "$1" || exit 1
COMP_WORDS=(%s)
COMP_CWORD=%d
COMP_LINE=%q
COMP_POINT=%d
__start_lstk
status=$?
printf '%%s\n' "${COMPREPLY[@]}"
exit $status
`, compWords, cword, line, len(line))
}

// TestBashCompletionWorksWithoutBashCompletionPackage guards against
// DEVX-950: on stock macOS (bash 3.2, no bash-completion package) every Tab
// press failed with "_get_comp_words_by_ref: command not found" because the
// Cobra-generated script depends on that function from the bash-completion
// package on both of its init paths.
func TestBashCompletionWorksWithoutBashCompletionPackage(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runBashCompletionDriver(t, completeInDriver("lstk st", 1, "lstk st"))
	require.NoError(t, err, "completion attempt failed\nstdout: %s\nstderr: %s", stdout, stderr)
	assert.NotContains(t, stderr, "command not found")

	completions := strings.Fields(stdout)
	assert.Contains(t, completions, "start")
	assert.Contains(t, completions, "status")
	assert.Contains(t, completions, "stop")
}

// TestBashCompletionAfterWhitespaceSeparatedFlagValue covers the shape that
// separates adjacency-aware reassembly from naive gluing: in
// 'lstk --config= st<TAB>' readline delivers the same COMP_WORDS as for
// 'lstk --config=st', and only COMP_LINE reveals that 'st' is a separate word
// that should be completed to a subcommand.
func TestBashCompletionAfterWhitespaceSeparatedFlagValue(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runBashCompletionDriver(t, completeInDriver(`lstk --config = st`, 3, "lstk --config= st"))
	require.NoError(t, err, "completion attempt failed\nstdout: %s\nstderr: %s", stdout, stderr)

	completions := strings.Fields(stdout)
	assert.Contains(t, completions, "start")
	assert.Contains(t, completions, "status")
	assert.Contains(t, completions, "stop")
}

// TestBashCompletionReassemblesWordbreakSplits verifies the self-contained
// _get_comp_words_by_ref fallback matches the bash-completion package's
// semantics: pieces split at COMP_WORDBREAKS characters are re-joined only
// when they were typed with no whitespace between them, which has to be
// recovered from COMP_LINE since COMP_WORDS is identical either way.
func TestBashCompletionReassemblesWordbreakSplits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		compWords string
		cword     int
		line      string
		expect    []string
	}{
		{
			name:      "adjacent pieces re-join",
			compWords: `lstk --config = ./cfg`,
			cword:     3,
			line:      "lstk --config=./cfg",
			expect:    []string{"cur=--config=./cfg", "prev=lstk", "cword=1", "nwords=2"},
		},
		{
			name:      "word after separator-then-space stays separate",
			compWords: `lstk --config = st`,
			cword:     3,
			line:      "lstk --config= st",
			expect:    []string{"cur=st", "prev=--config=", "cword=2", "nwords=3"},
		},
		{
			name:      "whitespace-surrounded separator stays separate",
			compWords: `lstk --config = ./x`,
			cword:     3,
			line:      "lstk --config = ./x",
			expect:    []string{"cur=./x", "prev==", "cword=3", "nwords=4"},
		},
		{
			name:      "empty word after separator-then-space",
			compWords: `lstk --config = ""`,
			cword:     3,
			line:      "lstk --config= ",
			expect:    []string{"cur=", "prev=--config=", "cword=2", "nwords=3"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			driver := fmt.Sprintf(`source "$1" || exit 1
COMP_WORDS=(%s)
COMP_CWORD=%d
COMP_LINE=%q
COMP_POINT=%d
run_reassembly() {
    local cur prev words cword
    _get_comp_words_by_ref -n =: cur prev words cword || exit 1
    printf 'cur=%%s\n' "$cur"
    printf 'prev=%%s\n' "$prev"
    printf 'cword=%%s\n' "$cword"
    printf 'nwords=%%s\n' "${#words[@]}"
}
run_reassembly
`, tc.compWords, tc.cword, tc.line, len(tc.line))

			stdout, stderr, err := runBashCompletionDriver(t, driver)
			require.NoError(t, err, "driver failed\nstdout: %s\nstderr: %s", stdout, stderr)
			assert.NotContains(t, stderr, "command not found")

			lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
			assert.Equal(t, tc.expect, lines)
		})
	}
}

// TestBashCompletionFallbackYieldsToInstalledPackage verifies the fallback is
// guarded: when the bash-completion package already provides
// _get_comp_words_by_ref, sourcing the lstk script must not replace it.
func TestBashCompletionFallbackYieldsToInstalledPackage(t *testing.T) {
	t.Parallel()

	driver := `_get_comp_words_by_ref() { echo "package version"; }
source "$1" || exit 1
_get_comp_words_by_ref
`
	stdout, stderr, err := runBashCompletionDriver(t, driver)
	require.NoError(t, err, "driver failed\nstdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stdout, "package version")
}
