package cmd

import "testing"

func TestVersionLine(t *testing.T) {
	got := versionLine()

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

	want := versionLine() + "\n"
	if longOut != want {
		t.Fatalf("--version output = %q, want %q", longOut, want)
	}
	if shortOut != longOut {
		t.Fatalf("-v output = %q, want %q", shortOut, longOut)
	}
}
