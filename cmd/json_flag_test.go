package cmd

import (
	"testing"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/telemetry"
)

func TestJSONFlagIsRegistered(t *testing.T) {
	root := NewRootCmd(&env.Env{}, telemetry.New("", true), log.Nop())

	flag := root.PersistentFlags().Lookup("json")
	if flag == nil {
		t.Fatal("expected --json flag to be registered on root command")
		return
	}
	if flag.DefValue != "false" {
		t.Fatalf("expected default value to be false, got %q", flag.DefValue)
	}
}

func TestJSONFlagAppearsInHelp(t *testing.T) {
	out, err := executeWithArgs(t, "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "--json")
}

func TestJSONFlagBindsToCfg(t *testing.T) {
	cfg := &env.Env{}
	root := NewRootCmd(cfg, telemetry.New("", true), log.Nop())
	root.SetArgs([]string{"--json", "--version"})
	_ = root.Execute()

	if !cfg.JSON {
		t.Fatal("expected cfg.JSON to be true after --json flag")
	}
}

func TestJSONFlagDefaultIsOff(t *testing.T) {
	cfg := &env.Env{}
	root := NewRootCmd(cfg, telemetry.New("", true), log.Nop())
	root.SetArgs([]string{"--version"})
	_ = root.Execute()

	if cfg.JSON {
		t.Fatal("expected cfg.JSON to be false when flag is not set")
	}
}

// isInteractiveMode short-circuits on cfg.JSON before consulting ui.IsInteractive,
// so this holds regardless of whether the test process itself has a TTY attached.
func TestIsInteractiveModeReturnsFalseWhenJSONSet(t *testing.T) {
	cfg := &env.Env{NonInteractive: false, JSON: true}
	if isInteractiveMode(cfg) {
		t.Fatal("expected isInteractiveMode to return false when cfg.JSON is true")
	}
}
