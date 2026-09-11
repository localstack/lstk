package cmd

import (
	"strings"
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

// The first-start tip says only `lstk completion` (PR #495 review), so that one
// command has to answer the whole question — these are the commands a user must
// be able to copy straight out of it. Asserted on the indented command lines
// only: wrapText reflows unindented prose to the terminal width.
var completionShellHelp = []struct {
	shell string
	title string
	lines []string
}{
	{"bash", "Bash:", []string{
		`eval "$(lstk completion bash)"`,
		`echo 'eval "$(lstk completion bash)"' >> ~/.bashrc`,
		`echo 'eval "$(lstk completion bash)"' >> ~/.bash_profile`,
	}},
	{"zsh", "Zsh:", []string{
		"source <(lstk completion zsh)",
		"echo 'autoload -Uz compinit && compinit' >> ~/.zshrc",
		"echo 'source <(lstk completion zsh)' >> ~/.zshrc",
	}},
	{"fish", "Fish:", []string{
		"lstk completion fish | source",
		"lstk completion fish > ~/.config/fish/completions/lstk.fish",
	}},
	{"powershell", "PowerShell:", []string{
		"lstk completion powershell | Out-String | Invoke-Expression",
		"lstk completion powershell | Out-File -Append -Encoding utf8 $PROFILE",
	}},
}

func TestCompletionHelpDocumentsEveryShell(t *testing.T) {
	out, err := executeWithArgs(t, "completion")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for _, tc := range completionShellHelp {
		assertContains(t, out, tc.title)
		for _, line := range tc.lines {
			assertContains(t, out, line)
		}
	}
	assertContains(t, out, "# Load in current session")
	assertContains(t, out, "# Load in new sessions (Linux)")
	assertContains(t, out, "# Load in new sessions (macOS)")

	// Process substitution is a silent no-op on stock macOS bash 3.2, and the
	// eval form needs no bash-completion package at all — that is what DEVX-950's
	// bundled fallback bought.
	assertNotContains(t, out, "source <(lstk completion bash)")
	assertNotContains(t, out, "bash_completion.d")

	// Cobra's zsh script calls compdef on line 2, which does not exist until
	// compinit has run: sourcing it first fails with "compdef: command not found"
	// and registers nothing.
	zsh := out[strings.Index(out, "Zsh:"):strings.Index(out, "Fish:")]
	for _, recipe := range strings.Split(zsh, "\n\n") {
		if strings.Contains(recipe, "completion zsh") && !strings.Contains(recipe, "compinit") {
			t.Fatalf("zsh recipe loads the script without compinit:\n%s", recipe)
		}
	}
}

// Per-shell help forwards rather than repeating the instructions, so there is
// one copy to read and one to maintain.
func TestCompletionShellHelpForwardsToParent(t *testing.T) {
	for _, tc := range completionShellHelp {
		out, err := executeWithArgs(t, "completion", tc.shell, "--help")
		if err != nil {
			t.Fatalf("completion %s --help: expected no error, got %v", tc.shell, err)
		}

		assertContains(t, out, "lstk completion --help")
		assertNotContains(t, out, "# Load in current session")
	}
}
