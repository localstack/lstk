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
