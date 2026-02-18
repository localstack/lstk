package cmd

import "testing"

func TestVersionLine(t *testing.T) {
	originalVersion := version
	originalCommit := commit
	originalBuildDate := buildDate
	t.Cleanup(func() {
		version = originalVersion
		commit = originalCommit
		buildDate = originalBuildDate
	})

	version = "2026.2.0"
	commit = "abc1234"
	buildDate = "2026-02-17T15:04:05Z"

	got := versionLine()
	want := "lstk 2026.2.0 (abc1234, 2026-02-17T15:04:05Z)"
	if got != want {
		t.Fatalf("versionLine() = %q, want %q", got, want)
	}
}
