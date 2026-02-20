package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type RuntimeUnavailableError struct {
	Summary string
	Detail  string
	cause   error
}

func (e *RuntimeUnavailableError) Error() string {
	return fmt.Sprintf("%s\n%s", e.Summary, e.Detail)
}

func (e *RuntimeUnavailableError) Unwrap() error {
	return e.cause
}

func AsRuntimeUnavailableError(err error) (*RuntimeUnavailableError, bool) {
	var out *RuntimeUnavailableError
	if errors.As(err, &out) {
		return out, true
	}
	return nil, false
}

type connectionChecker interface {
	CheckConnection(ctx context.Context) error
}

// CheckContainerEngine validates that the runtime can reach the container engine.
func CheckContainerEngine(ctx context.Context, rt Runtime) error {
	checker, ok := rt.(connectionChecker)
	if !ok {
		return nil
	}
	return checker.CheckConnection(ctx)
}

func formatDockerConnectionError(endpoint string, err error) error {
	if err == nil {
		return nil
	}

	msg := strings.ToLower(err.Error())
	if isDockerDaemonUnavailable(msg) {
		return &RuntimeUnavailableError{
			Summary: "No container runtime available or running.",
			Detail:  err.Error(),
			cause:   err,
		}
	}

	return fmt.Errorf("docker engine check failed at %s: %w", endpoint, err)
}

func isDockerDaemonUnavailable(msg string) bool {
	return strings.Contains(msg, "cannot connect to the docker daemon") ||
		strings.Contains(msg, "is the docker daemon running")
}
