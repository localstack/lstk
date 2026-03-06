package cmd

import (
	"encoding/json"
	"testing"
)

func TestDescribeOutputIsValidJSON(t *testing.T) {
	out, err := executeWithArgs(t, "describe")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var desc commandDesc
	if err := json.Unmarshal([]byte(out), &desc); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput:\n%s", err, out)
	}

	if desc.Name != "lstk" {
		t.Fatalf("expected root command name 'lstk', got %q", desc.Name)
	}

	if len(desc.Commands) == 0 {
		t.Fatal("expected subcommands in describe output")
	}

	cmdNames := make(map[string]bool)
	for _, sub := range desc.Commands {
		cmdNames[sub.Name] = true
	}

	for _, want := range []string{"start", "stop", "login", "logout", "logs", "config", "version"} {
		if !cmdNames[want] {
			t.Errorf("expected command %q in describe output", want)
		}
	}

	for _, excluded := range []string{"describe", "completion", "help"} {
		if cmdNames[excluded] {
			t.Errorf("%s command should not appear in describe output", excluded)
		}
	}
}

func TestDescribeIncludesFlags(t *testing.T) {
	out, err := executeWithArgs(t, "describe")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var desc commandDesc
	if err := json.Unmarshal([]byte(out), &desc); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}

	var logsCmd *commandDesc
	for i, sub := range desc.Commands {
		if sub.Name == "logs" {
			logsCmd = &desc.Commands[i]
			break
		}
	}

	if logsCmd == nil {
		t.Fatal("expected logs command in output")
	}

	foundFollow := false
	for _, f := range logsCmd.Flags {
		if f.Name == "follow" {
			foundFollow = true
			if f.Shorthand != "f" {
				t.Errorf("expected follow shorthand 'f', got %q", f.Shorthand)
			}
		}
	}

	if !foundFollow {
		t.Error("expected --follow flag on logs command")
	}
}
