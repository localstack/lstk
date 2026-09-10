package cmd

import (
	"testing"
)

// TestCompletionBashWritesFallbackAndScriptToSameWriter guards the DEVX-950
// wiring: the fallback prelude and Cobra's generated script must both reach
// the writer configured at execution time. Cobra captures its output writer
// when InitDefaultCompletionCmd runs (before SetOut is called here), so a
// prepend-and-delegate wrapper would send the two halves to different
// destinations.
func TestCompletionBashWritesFallbackAndScriptToSameWriter(t *testing.T) {
	out, err := executeWithArgs(t, "completion", "bash")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "_get_comp_words_by_ref()")
	assertContains(t, out, "__start_lstk")
	assertContains(t, out, "__complete")
}

// TestCompletionBashNoDescriptionsFlagStillHonored verifies the wrapped RunE
// keeps Cobra's --no-descriptions behavior: the generated script requests
// completions via __completeNoDesc instead of __complete.
func TestCompletionBashNoDescriptionsFlagStillHonored(t *testing.T) {
	out, err := executeWithArgs(t, "completion", "bash", "--no-descriptions")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "_get_comp_words_by_ref()")
	assertContains(t, out, "__completeNoDesc")
}

// completionShellHelp is what a user must be able to copy straight out of the
// help for each shell. Asserted on the indented command lines only: wrapText
// reflows unindented prose to the terminal width, so prose is not stable text.
var completionShellHelp = []struct {
	shell   string
	title   string
	load    string
	persist string
}{
	{"bash", "Bash:", `eval "$(lstk completion bash)"`, "~/.local/share/bash-completion/completions/lstk"},
	{"zsh", "Zsh:", "source <(lstk completion zsh)", `lstk completion zsh > "${fpath[1]}/_lstk"`},
	{"fish", "Fish:", "lstk completion fish | source", "~/.config/fish/completions/lstk.fish"},
	{"powershell", "PowerShell:", "lstk completion powershell | Out-String | Invoke-Expression", "$PROFILE"},
}

// The tip on first start now says only `lstk completion` (PR #495 review), so
// that command has to carry the setup instructions the tip used to link to.
func TestCompletionHelpDocumentsEveryShell(t *testing.T) {
	out, err := executeWithArgs(t, "completion")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for _, tc := range completionShellHelp {
		assertContains(t, out, tc.title)
		assertContains(t, out, tc.load)
		assertContains(t, out, tc.persist)
	}
}

func TestCompletionHelpOffersLoadAndPersistPerOS(t *testing.T) {
	out, err := executeWithArgs(t, "completion")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertContains(t, out, "# Load in current session")
	assertContains(t, out, "# Persist (Linux")
	assertContains(t, out, "# Persist (macOS")
	assertContains(t, out, "# Persist (Windows")
}

// Process substitution is a silent no-op on stock macOS bash 3.2, so the help
// must never recommend it for bash (DEVX-950).
func TestCompletionHelpNeverRecommendsSourceForBash(t *testing.T) {
	out, err := executeWithArgs(t, "completion")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertNotContains(t, out, "source <(lstk completion bash)")
}

// Homebrew wires completion up on its own (homebrew_casks.completions), so its
// paths are noise in instructions aimed at everyone else.
func TestCompletionHelpOmitsHomebrewSetup(t *testing.T) {
	out, err := executeWithArgs(t, "completion")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertNotContains(t, out, "brew --prefix")
}

// Each shell's own help and the parent's must show the same instructions, so a
// user gets one answer whichever they run.
func TestCompletionShellHelpMatchesParentHelp(t *testing.T) {
	for _, tc := range completionShellHelp {
		out, err := executeWithArgs(t, "completion", tc.shell, "--help")
		if err != nil {
			t.Fatalf("completion %s --help: expected no error, got %v", tc.shell, err)
		}

		assertContains(t, out, tc.load)
		assertContains(t, out, tc.persist)
	}
}
