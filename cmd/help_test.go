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

func TestRootHelpIncludesDoctorCommand(t *testing.T) {
	out, err := executeWithArgs(t, "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertContains(t, out, "doctor")
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
