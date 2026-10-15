package container

import (
	"context"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/runtime"
)

const (
	// shutdownTimeoutEnv names the variable that sets how many seconds
	// LocalStack's supervisor gives the emulator to shut down before it kills
	// it.
	shutdownTimeoutEnv = "SHUTDOWN_TIMEOUT"
	// prefixedShutdownTimeoutEnv overrides it: the image's docker-entrypoint.sh
	// re-exports every LOCALSTACK_<NAME> variable as <NAME>, and lstk forwards
	// the host's LOCALSTACK_* variables into the container.
	prefixedShutdownTimeoutEnv = "LOCALSTACK_" + shutdownTimeoutEnv
	// defaultShutdownTimeout is the supervisor's value when the variable is
	// unset or blank.
	defaultShutdownTimeout = 5
	// stopTimeoutMargin lets the supervisor finish its kill and exit before
	// Docker kills the container.
	stopTimeoutMargin = 5
	// noKillStopTimeout is the stop timeout of an emulator whose supervisor
	// never kills it (SHUTDOWN_TIMEOUT 0 or less) or whose container says to
	// wait forever (a stop timeout of -1): lstk bounds the stop, so a hung
	// emulator cannot hold it indefinitely.
	noKillStopTimeout = 300
	// maxStopTimeout bounds larger values, so the arithmetic on them stays in
	// range on every platform.
	maxStopTimeout = math.MaxInt32
	// minStopWait keeps the 30 s lstk waited for every stop before containers
	// carried a stop timeout of their own.
	minStopWait = 30 * time.Second
	// stopWaitMargin is how much longer than the container's stop timeout lstk
	// waits for the stop and the removal that follows it to come back.
	stopWaitMargin = 15 * time.Second
)

// stopTimeoutFor returns the Docker stop timeout of the container lstk
// creates from c: the emulator's SHUTDOWN_TIMEOUT as the container will see
// it, plus a margin. Docker's own default of 10 s would kill an emulator
// whose shutdown still runs, such as the Azure emulator removing the
// containers it started.
func stopTimeoutFor(ctx context.Context, rt runtime.Runtime, c runtime.ContainerConfig) int {
	// The container's variables override the image's: listed after them, they
	// are the last values. An image that cannot be read leaves the container's.
	imageEnv, _ := rt.ImageEnv(ctx, c.Image)
	return stopTimeoutFromEnv(append(slices.Clone(imageEnv), c.Env...))
}

// containerStopTimeout returns how many seconds Docker waits for the named
// container to stop: its own stop timeout, raised to the one its environment
// gives as at start. A container an older lstk created, or one started outside
// lstk, has none of its own, and Podman reports its 10 s default for every
// container. nil when neither can be read, which leaves Docker's.
func containerStopTimeout(ctx context.Context, rt runtime.Runtime, name string) *int {
	var own *int
	if brief, err := rt.InspectBrief(ctx, name); err == nil && brief.StopTimeout != nil {
		seconds := *brief.StopTimeout
		if seconds < 0 {
			seconds = noKillStopTimeout
		}
		seconds = min(seconds, maxStopTimeout)
		own = &seconds
	}
	env, err := rt.ContainerEnv(ctx, name)
	if err != nil {
		return own
	}
	derived := stopTimeoutFromEnv(env)
	if own != nil && *own >= derived {
		return own
	}
	return &derived
}

// stopTimeoutFromEnv returns the stop timeout of a container whose
// environment is env; the last value of a repeated variable wins, as in the
// container's shell.
func stopTimeoutFromEnv(env []string) int {
	seconds := supervisorShutdownTimeout(env)
	switch {
	case seconds <= 0:
		return noKillStopTimeout
	case seconds > maxStopTimeout-stopTimeoutMargin:
		return maxStopTimeout
	default:
		return seconds + stopTimeoutMargin
	}
}

// supervisorShutdownTimeout returns the SHUTDOWN_TIMEOUT the supervisor reads
// in a container whose environment is env. A LOCALSTACK_SHUTDOWN_TIMEOUT
// replaces it even when blank. Python's rarer integer spellings, such as
// 1_000, get the default here, as a value that is no integer does.
func supervisorShutdownTimeout(env []string) int {
	name := shutdownTimeoutEnv
	if envHasKey(env, prefixedShutdownTimeoutEnv) {
		name = prefixedShutdownTimeoutEnv
	}
	value := strings.TrimSpace(envValue(env, name))
	if value == "" {
		return defaultShutdownTimeout
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return defaultShutdownTimeout
	}
	// Out of range, ParseInt returns the nearest bound.
	return int(min(max(seconds, math.MinInt32), math.MaxInt32))
}

// stopWait is how long lstk waits for a stop in which Docker waits
// stopTimeout seconds for the emulator to shut down: that plus a margin, and
// never less than minStopWait. nil, Docker's own stop timeout, gets the
// minimum.
func stopWait(stopTimeout *int) time.Duration {
	if stopTimeout == nil {
		return minStopWait
	}
	return max(time.Duration(*stopTimeout)*time.Second+stopWaitMargin, minStopWait)
}
