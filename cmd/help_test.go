package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func executeWithArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()

	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)

	err := rootCmd.ExecuteContext(context.Background())
	out := buf.String()

	rootCmd.SetArgs(nil)
	rootCmd.SetOut(origOut)
	rootCmd.SetErr(origErr)

	return out, err
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
