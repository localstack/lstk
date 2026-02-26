package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func executeWithArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()

	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	resetCommandState(rootCmd)

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)

	err := rootCmd.ExecuteContext(context.Background())
	out := buf.String()

	rootCmd.SetArgs(nil)
	rootCmd.SetOut(origOut)
	rootCmd.SetErr(origErr)
	resetCommandState(rootCmd)

	return out, err
}

func resetCommandState(cmd *cobra.Command) {
	resetFlagSet(cmd.Flags())
	resetFlagSet(cmd.PersistentFlags())

	for _, sub := range cmd.Commands() {
		resetCommandState(sub)
	}
}

func resetFlagSet(flags *pflag.FlagSet) {
	flags.VisitAll(func(flag *pflag.Flag) {
		_ = flag.Value.Set(flag.DefValue)
		flag.Changed = false
	})
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
