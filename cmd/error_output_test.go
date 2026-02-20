package cmd

import "testing"

func TestSummarizeRuntimeDetail_DockerDaemonMessage(t *testing.T) {
	input := "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"
	got := summarizeRuntimeDetail(input)
	if got != "Cannot connect to Docker daemon." {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestSummarizeRuntimeDetail_Passthrough(t *testing.T) {
	input := "permission denied while trying to connect to the docker API"
	got := summarizeRuntimeDetail(input)
	if got != input {
		t.Fatalf("expected passthrough, got %q", got)
	}
}
