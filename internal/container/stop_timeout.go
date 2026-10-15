package container

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/runtime"
)

const (
	// shutdownTimeoutEnv is how many seconds LocalStack's supervisor gives the
	// emulator to shut down before it kills it.
	shutdownTimeoutEnv = "SHUTDOWN_TIMEOUT"
	// defaultShutdownTimeout is the supervisor's own default for it.
	defaultShutdownTimeout = 5
	// stopTimeoutMargin lets the supervisor finish its kill and exit before
	// Docker kills the container.
	stopTimeoutMargin = 5
	// minStopWait is how long lstk waits for a stop at least.
	minStopWait = 30 * time.Second
	// stopWaitMargin is how much longer than the container's stop timeout lstk
	// waits for the stop and the removal that follows it to come back.
	stopWaitMargin = 15 * time.Second
)

// stopTimeoutFor returns the container's Docker stop timeout: the emulator's
// SHUTDOWN_TIMEOUT, from the container env or else the image env, plus a
// margin. Docker's own default of 10 s would kill an emulator whose shutdown
// still runs, such as the Azure emulator removing the containers it started.
func stopTimeoutFor(ctx context.Context, rt runtime.Runtime, c runtime.ContainerConfig) int {
	seconds, ok := shutdownTimeoutFromEnv(c.Env)
	if !ok {
		if imageEnv, err := rt.ImageEnv(ctx, c.Image); err == nil {
			seconds, ok = shutdownTimeoutFromEnv(imageEnv)
		}
	}
	if !ok {
		seconds = defaultShutdownTimeout
	}
	return seconds + stopTimeoutMargin
}

// shutdownTimeoutFromEnv returns the last SHUTDOWN_TIMEOUT in env, as Docker
// applies the last value of a variable set twice. A value that is no positive
// number counts as unset.
func shutdownTimeoutFromEnv(env []string) (int, bool) {
	seconds, found := 0, false
	for _, entry := range env {
		value, ok := strings.CutPrefix(entry, shutdownTimeoutEnv+"=")
		if !ok {
			continue
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			seconds, found = 0, false
			continue
		}
		seconds, found = parsed, true
	}
	return seconds, found
}

// stopWait is how long lstk waits for a container's stop: its stop timeout,
// which Docker waits for the emulator to shut down, plus a margin.
func stopWait(brief runtime.ContainerBrief) time.Duration {
	return max(time.Duration(brief.StopTimeout)*time.Second+stopWaitMargin, minStopWait)
}
