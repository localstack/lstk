package cmd

import (
	"testing"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/telemetry"
)

func TestNonInteractiveFlagIsRegistered(t *testing.T) {
	root := NewRootCmd(&env.Env{}, telemetry.New("", true))

	flag := root.PersistentFlags().Lookup("non-interactive")
	if flag == nil {
		t.Fatal("expected --non-interactive flag to be registered on root command")
	}
	if flag.DefValue != "false" {
		t.Fatalf("expected default value to be false, got %q", flag.DefValue)
	}
}

func TestNonInteractiveFlagAppearsInHelp(t *testing.T) {
	out, err := executeWithArgs(t, "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "--non-interactive")
}

func TestNonInteractiveFlagAppearsInStartHelp(t *testing.T) {
	out, err := executeWithArgs(t, "start", "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "--non-interactive")
}

func TestNonInteractiveFlagAppearsInStopHelp(t *testing.T) {
	out, err := executeWithArgs(t, "stop", "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "--non-interactive")
}

func TestNonInteractiveFlagBindsToCfg(t *testing.T) {
	cfg := &env.Env{}
	root := NewRootCmd(cfg, telemetry.New("", true))
	root.SetArgs([]string{"--non-interactive", "version"})
	_ = root.Execute()

	if !cfg.NonInteractive {
		t.Fatal("expected cfg.NonInteractive to be true after --non-interactive flag")
	}
}

func TestNonInteractiveFlagDefaultIsOff(t *testing.T) {
	cfg := &env.Env{}
	root := NewRootCmd(cfg, telemetry.New("", true))
	root.SetArgs([]string{"version"})
	_ = root.Execute()

	if cfg.NonInteractive {
		t.Fatal("expected cfg.NonInteractive to be false when flag is not set")
	}
}
