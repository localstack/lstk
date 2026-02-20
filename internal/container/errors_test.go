package container

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatContainerNameConflict_DockerMessage(t *testing.T) {
	name := "localstack-aws"
	err := errors.New(`Conflict. The container name "/localstack-aws" is already in use by container "123".`)

	got := formatContainerNameConflict(name, err)
	if got == nil {
		t.Fatal("expected error")
	}
	msg := got.Error()
	if !strings.Contains(msg, "container name conflict") {
		t.Fatalf("expected conflict message, got %q", msg)
	}
	if !strings.Contains(msg, "docker rm -f localstack-aws") {
		t.Fatalf("expected remediation command, got %q", msg)
	}
}

func TestFormatContainerNameConflict_NonConflict(t *testing.T) {
	err := errors.New("some other docker error")
	got := formatContainerNameConflict("localstack-aws", err)
	if !errors.Is(got, err) {
		t.Fatalf("expected original error, got %v", got)
	}
}
