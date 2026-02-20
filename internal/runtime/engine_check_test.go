package runtime

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeRuntime struct {
	err error
}

func (f *fakeRuntime) PullImage(context.Context, string, chan<- PullProgress) error { return nil }
func (f *fakeRuntime) Start(context.Context, ContainerConfig) (string, error)       { return "", nil }
func (f *fakeRuntime) Stop(context.Context, string) error                           { return nil }
func (f *fakeRuntime) Remove(context.Context, string) error                         { return nil }
func (f *fakeRuntime) IsRunning(context.Context, string) (bool, error)              { return false, nil }
func (f *fakeRuntime) Logs(context.Context, string, int) (string, error)            { return "", nil }
func (f *fakeRuntime) StreamLogs(context.Context, string, io.Writer) error          { return nil }
func (f *fakeRuntime) GetImageVersion(context.Context, string) (string, error)      { return "", nil }
func (f *fakeRuntime) CheckConnection(context.Context) error                        { return f.err }

func TestFormatDockerConnectionErrorFriendly(t *testing.T) {
	endpoint := "unix:///Users/geo/.docker/run/docker.sock"
	original := errors.New("Cannot connect to the Docker daemon at unix:///Users/geo/.docker/run/docker.sock. Is the docker daemon running?")

	err := formatDockerConnectionError(endpoint, original)
	if err == nil {
		t.Fatal("expected error")
	}

	runtimeErr, ok := AsRuntimeUnavailableError(err)
	if !ok {
		t.Fatalf("expected RuntimeUnavailableError, got %T", err)
	}
	if runtimeErr.Summary != "No container runtime available or running." {
		t.Fatalf("unexpected summary: %q", runtimeErr.Summary)
	}
	if runtimeErr.Detail != original.Error() {
		t.Fatalf("unexpected detail: %q", runtimeErr.Detail)
	}
}

func TestFormatDockerConnectionErrorUnavailable(t *testing.T) {
	endpoint := "unix:///var/run/docker.sock"
	original := errors.New("dial unix /var/run/docker.sock: connect: no such file or directory")

	err := formatDockerConnectionError(endpoint, original)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := AsRuntimeUnavailableError(err); ok {
		t.Fatalf("did not expect RuntimeUnavailableError for non-daemon error")
	}
	if !strings.Contains(err.Error(), "docker engine check failed at") {
		t.Fatalf("expected generic engine check error, got %q", err.Error())
	}
}

func TestFormatDockerConnectionErrorPermission(t *testing.T) {
	endpoint := "unix:///Users/geo/.docker/run/docker.sock"
	original := errors.New("permission denied while trying to connect to the docker API at unix:///Users/geo/.docker/run/docker.sock")

	err := formatDockerConnectionError(endpoint, original)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := AsRuntimeUnavailableError(err); ok {
		t.Fatalf("did not expect RuntimeUnavailableError for non-daemon error")
	}
}

func TestCheckContainerEngineUsesChecker(t *testing.T) {
	expected := errors.New("boom")
	rt := &fakeRuntime{err: expected}

	err := CheckContainerEngine(context.Background(), rt)
	if !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}
