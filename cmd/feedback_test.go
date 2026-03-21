package cmd

import (
	"context"
	"testing"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/telemetry"
)

func TestFeedbackCommandAppearsInHelp(t *testing.T) {
	out, err := executeWithArgs(t, "--help")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertContains(t, out, "feedback")
}

func TestFeedbackCommandRequiresInteractiveTerminal(t *testing.T) {
	root := NewRootCmd(&env.Env{
		NonInteractive: true,
		AuthToken:      "Bearer auth-token",
	}, telemetry.New("", true), log.Nop())
	root.SetArgs([]string{"feedback"})

	err := root.ExecuteContext(context.Background())
	if err == nil || err.Error() != "feedback requires an interactive terminal" {
		t.Fatalf("expected interactive terminal error, got %v", err)
	}
}
