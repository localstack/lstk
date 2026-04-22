package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/telemetry"
)

// --- help / registration tests (no auth, no Docker required) ---

func TestSnapshotCommandIsRegistered(t *testing.T) {
	out, err := executeWithArgs(t, "snapshot", "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "snapshot")
	assertContains(t, out, "save")
	assertContains(t, out, "load")
	assertContains(t, out, "export")
	assertContains(t, out, "import")
	assertContains(t, out, "list")
	assertContains(t, out, "delete")
	assertContains(t, out, "versions")
}

func TestSnapshotSaveHelp(t *testing.T) {
	out, err := executeWithArgs(t, "snapshot", "save", "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "NAME")
	assertContains(t, out, "--services")
	assertContains(t, out, "--message")
	assertContains(t, out, "--visibility")
}

func TestSnapshotLoadHelp(t *testing.T) {
	out, err := executeWithArgs(t, "snapshot", "load", "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "SOURCE")
	assertContains(t, out, "--strategy")
	assertContains(t, out, "--dry-run")
}

func TestSnapshotExportHelp(t *testing.T) {
	out, err := executeWithArgs(t, "snapshot", "export", "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "--services")
}

func TestSnapshotDeleteHelp(t *testing.T) {
	out, err := executeWithArgs(t, "snapshot", "delete", "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "--yes")
}

// --- auth guard tests (no Docker required) ---

func TestSnapshotSaveRequiresAuth(t *testing.T) {
	out, err := executeWithArgs(t, "--non-interactive", "snapshot", "save", "my-pod")
	if err == nil {
		t.Fatal("expected error when not authenticated, got nil")
	}
	_ = out
}

func TestSnapshotLoadRequiresAuth(t *testing.T) {
	_, err := executeWithArgs(t, "--non-interactive", "snapshot", "load", "my-pod")
	if err == nil {
		t.Fatal("expected error when not authenticated, got nil")
	}
}

func TestSnapshotListRequiresAuth(t *testing.T) {
	_, err := executeWithArgs(t, "--non-interactive", "snapshot", "list")
	if err == nil {
		t.Fatal("expected error when not authenticated, got nil")
	}
}

func TestSnapshotDeleteRequiresAuth(t *testing.T) {
	_, err := executeWithArgs(t, "--non-interactive", "snapshot", "delete", "--yes", "my-pod")
	if err == nil {
		t.Fatal("expected error when not authenticated, got nil")
	}
}

func TestSnapshotVersionsRequiresAuth(t *testing.T) {
	_, err := executeWithArgs(t, "--non-interactive", "snapshot", "versions", "my-pod")
	if err == nil {
		t.Fatal("expected error when not authenticated, got nil")
	}
}

func TestSnapshotDeleteRequiresConfirmInNonInteractiveMode(t *testing.T) {
	// Auth is checked first, so this fails with an auth error in the test env
	// (no token available). The --yes check is covered by TestSnapshotDeleteYesFlagBehavior.
	_, err := executeWithArgs(t, "--non-interactive", "snapshot", "delete", "my-pod")
	if err == nil {
		t.Fatal("expected error in non-interactive mode without auth, got nil")
	}
}

func TestSnapshotDeleteYesFlagBehavior(t *testing.T) {
	// Verify the --yes flag is registered and the command fails with a clear message
	// when omitting it in non-interactive mode with a fake token.
	cfg := &env.Env{
		AuthToken:      "fake-token-for-test",
		NonInteractive: true,
		APIEndpoint:    "https://api.localstack.cloud",
	}
	buf := new(bytes.Buffer)
	root := NewRootCmd(cfg, telemetry.New("", true), log.Nop())
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"snapshot", "delete", "my-pod"})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error when --yes not provided in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected error to mention --yes, got: %v", err)
	}
}

// --- parsePodVersion unit tests ---

func TestParsePodVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input       string
		wantName    string
		wantVersion int
	}{
		{"my-pod", "my-pod", 0},
		{"my-pod:3", "my-pod", 3},
		{"my-pod:0", "my-pod:0", 0}, // version 0 is invalid; treated as part of name
		{"my-pod:", "my-pod:", 0},
		{"my-pod:abc", "my-pod:abc", 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			name, version := parsePodVersion(tt.input)
			if name != tt.wantName || version != tt.wantVersion {
				t.Fatalf("parsePodVersion(%q) = (%q, %d), want (%q, %d)",
					tt.input, name, version, tt.wantName, tt.wantVersion)
			}
		})
	}
}

func TestSplitServices(t *testing.T) {
	t.Parallel()

	got := splitServices("s3, lambda , dynamodb")
	want := []string{"s3", "lambda", "dynamodb"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
