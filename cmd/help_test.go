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

func executeWithArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	cmd := NewRootCmd(&env.Env{}, telemetry.New("", true), log.Nop())
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestRootHelpOutputTemplate(t *testing.T) {
	out, err := executeWithArgs(t, "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertContains(t, out, "Usage:")
	assertContains(t, out, "lstk [options] [command]")
	assertContains(t, out, "LSTK - LocalStack command-line interface")
	assertContains(t, out, "Commands:")
	assertContains(t, out, "Options:")
	assertNotContains(t, out, "Available Commands:")
	assertNotContains(t, out, `Use "lstk [command] --help" for more information about a command.`)
	assertNotContains(t, out, "\n  version ")
}

func TestRootHelpGroupsToolsSeparately(t *testing.T) {
	out, err := executeWithArgs(t, "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertContains(t, out, "Commands:")
	assertContains(t, out, "Tools:")

	// The proxy commands must be listed under the Tools group, not among the
	// regular commands.
	toolsSection := out[strings.Index(out, "Tools:"):]
	for _, tool := range []string{"aws", "az", "cdk", "eksctl", "sam", "terraform"} {
		assertContains(t, toolsSection, tool)
	}

	// Tools come after the management commands in the help output.
	if strings.Index(out, "Commands:") > strings.Index(out, "Tools:") {
		t.Fatalf("expected Commands group to appear before Tools group\noutput:\n%s", out)
	}

	// No commands should fall through to an ungrouped section.
	assertNotContains(t, out, "Additional Commands:")
}

func TestSubcommandHelpUsesSubcommandUsageLine(t *testing.T) {
	out, err := executeWithArgs(t, "start", "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertContains(t, out, "Start emulator and services.")
	assertContains(t, out, "Usage:")
	assertContains(t, out, "lstk start")
	assertContains(t, out, "Options:")
	assertNotContains(t, out, "LSTK - LocalStack command-line interface")
}

func TestSaveLoadHelpShowsInvokedCommandForm(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantContains    string
		wantNotContains string
	}{
		{
			name:            "save alias shows save examples",
			args:            []string{"save", "--help"},
			wantContains:    "lstk save ./my-snapshot.snapshot",
			wantNotContains: "lstk snapshot save",
		},
		{
			name:            "load alias shows load examples",
			args:            []string{"load", "--help"},
			wantContains:    "lstk load my-baseline",
			wantNotContains: "lstk snapshot load",
		},
		{
			name:         "snapshot save keeps canonical form",
			args:         []string{"snapshot", "save", "--help"},
			wantContains: "lstk snapshot save ./my-snapshot.snapshot",
		},
		{
			name:         "snapshot load keeps canonical form",
			args:         []string{"snapshot", "load", "--help"},
			wantContains: "lstk snapshot load my-baseline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := executeWithArgs(t, tt.args...)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			assertContains(t, out, tt.wantContains)
			if tt.wantNotContains != "" {
				assertNotContains(t, out, tt.wantNotContains)
			}
		})
	}
}

func assertContains(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Fatalf("expected output to contain %q\noutput:\n%s", want, s)
	}
}

func assertNotContains(t *testing.T, s, want string) {
	t.Helper()
	if strings.Contains(s, want) {
		t.Fatalf("expected output not to contain %q\noutput:\n%s", want, s)
	}
}
