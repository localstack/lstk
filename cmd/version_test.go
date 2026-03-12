package cmd

import "testing"

func TestVersionLine(t *testing.T) {
	got := versionLine()

	if got == "" {
		t.Fatal("versionLine() should not be empty")
	}
	if got != "lstk dev" {
		t.Fatalf("versionLine() = %q, want %q", got, "lstk dev")
	}
}

func TestVersionFlagsPrintSameOutput(t *testing.T) {
	longOut, err := executeWithArgs(t, "--version")
	if err != nil {
		t.Fatalf("expected no error from --version, got %v", err)
	}

	shortOut, err := executeWithArgs(t, "-v")
	if err != nil {
		t.Fatalf("expected no error from -v, got %v", err)
	}

	if longOut != versionTemplate() {
		t.Fatalf("--version output = %q, want %q", longOut, versionTemplate())
	}
	if shortOut != longOut {
		t.Fatalf("-v output = %q, want %q", shortOut, longOut)
	}
}
