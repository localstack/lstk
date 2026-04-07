package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

const (
	doctorTimeout          = 30 * time.Second
	dockerCheckTimeout     = 5 * time.Second
	emulatorInspectTimeout = 3 * time.Second
	emulatorHealthTimeout  = 5 * time.Second
)

const (
	statusOK   = "OK"
	statusWarn = "WARN"
	statusFail = "FAIL"
)

type ConfigState struct {
	Path      string
	Exists    bool
	Loaded    bool
	LoadError error
}

type Options struct {
	Config           ConfigState
	Containers       []config.ContainerConfig
	LocalStackHost   string
	EnvAuthToken     string
	ForceFileKeyring bool
	Logger           log.Logger
	RuntimeInitError error
}

type row struct {
	check  string
	status string
	detail string
}

func Run(ctx context.Context, rt runtime.Runtime, emulatorClient emulator.Client, sink output.Sink, opts Options) error {
	ctx, cancel := context.WithTimeout(ctx, doctorTimeout)
	defer cancel()

	var rows []row
	output.EmitInfo(sink, "Checking configuration")
	rows = append(rows, configRow(opts.Config))
	output.EmitInfo(sink, "Checking Docker runtime")
	dockerStatus, dockerReady := dockerRow(ctx, rt, opts.RuntimeInitError)
	rows = append(rows, dockerStatus)
	output.EmitInfo(sink, "Checking authentication")
	rows = append(rows, authRows(opts)...)

	if opts.Config.Loaded && len(opts.Containers) > 0 {
		output.EmitInfo(sink, "Checking configured emulators")
		rows = append(rows, emulatorRows(ctx, rt, emulatorClient, opts.Containers, opts.LocalStackHost, dockerReady)...)
	} else if opts.Config.Loaded && len(opts.Containers) == 0 {
		rows = append(rows, row{
			check:  "Configured emulators",
			status: statusWarn,
			detail: "No emulators defined in config",
		})
	}

	tableRows := make([][]string, 0, len(rows))
	failures := 0
	warnings := 0
	for _, r := range rows {
		tableRows = append(tableRows, []string{r.check, r.status, r.detail})
		switch r.status {
		case statusFail:
			failures++
		case statusWarn:
			warnings++
		}
	}

	output.EmitInfo(sink, "Running LocalStack diagnostics")
	output.EmitTable(sink, output.TableEvent{
		Headers: []string{"Check", "Status", "Detail"},
		Rows:    tableRows,
	})

	switch {
	case failures > 0:
		output.EmitWarning(sink, "Doctor found issues that require attention")
		return output.NewSilentError(errors.New("doctor found issues"))
	case warnings > 0:
		output.EmitNote(sink, "Doctor completed with warnings")
	default:
		output.EmitSuccess(sink, "Doctor found no issues")
	}

	return nil
}

func configRow(state ConfigState) row {
	switch {
	case state.LoadError != nil:
		return row{
			check:  "Config file",
			status: statusFail,
			detail: fmt.Sprintf("%s (%v)", fallbackValue(state.Path, "unknown"), state.LoadError),
		}
	case state.Exists:
		return row{
			check:  "Config file",
			status: statusOK,
			detail: state.Path,
		}
	default:
		return row{
			check:  "Config file",
			status: statusWarn,
			detail: fmt.Sprintf("Not found at %s; doctor is read-only and will not create it", fallbackValue(state.Path, "unknown")),
		}
	}
}

func dockerRow(ctx context.Context, rt runtime.Runtime, initErr error) (row, bool) {
	detail := dockerDetail(rt)
	if initErr != nil {
		return row{
			check:  "Docker runtime",
			status: statusFail,
			detail: fmt.Sprintf("%s (%v)", detail, initErr),
		}, false
	}

	if rt == nil {
		return row{
			check:  "Docker runtime",
			status: statusFail,
			detail: "Runtime is not initialized",
		}, false
	}

	checkCtx, cancel := context.WithTimeout(ctx, dockerCheckTimeout)
	defer cancel()

	if err := rt.IsHealthy(checkCtx); err != nil {
		if timedOut(checkCtx, err) {
			return row{
				check:  "Docker runtime",
				status: statusFail,
				detail: fmt.Sprintf("%s (health check timed out after %s)", detail, dockerCheckTimeout),
			}, false
		}
		return row{
			check:  "Docker runtime",
			status: statusFail,
			detail: fmt.Sprintf("%s (%v)", detail, err),
		}, false
	}

	return row{
		check:  "Docker runtime",
		status: statusOK,
		detail: detail,
	}, true
}

func authRows(opts Options) []row {
	rows := []row{{
		check:  "LOCALSTACK_AUTH_TOKEN",
		status: statusWarn,
		detail: "Not set",
	}}
	if opts.EnvAuthToken != "" {
		rows[0].status = statusOK
		rows[0].detail = "Set in the environment"
	}

	storage, err := auth.NewTokenStorage(opts.ForceFileKeyring, opts.Logger)
	if err != nil {
		rows = append(rows, row{
			check:  "Stored auth token",
			status: statusWarn,
			detail: fmt.Sprintf("Could not inspect token storage: %v", err),
		})
		return rows
	}

	token, err := storage.GetAuthToken()
	switch {
	case err == nil && token != "":
		detail := "Token available in local storage"
		if opts.ForceFileKeyring {
			detail = "Token available in file storage"
		}
		rows = append(rows, row{
			check:  "Stored auth token",
			status: statusOK,
			detail: detail,
		})
	case errors.Is(err, auth.ErrTokenNotFound):
		if opts.EnvAuthToken != "" {
			return rows
		}
		rows = append(rows, row{
			check:  "Stored auth token",
			status: statusWarn,
			detail: "Not found in local storage",
		})
	default:
		rows = append(rows, row{
			check:  "Stored auth token",
			status: statusWarn,
			detail: fmt.Sprintf("Could not read token storage: %v", err),
		})
	}

	return rows
}

func emulatorRows(ctx context.Context, rt runtime.Runtime, emulatorClient emulator.Client, containers []config.ContainerConfig, localStackHost string, dockerReady bool) []row {
	if !dockerReady || rt == nil {
		return []row{{
			check:  "Configured emulators",
			status: statusWarn,
			detail: "Skipping emulator checks until Docker is available",
		}}
	}

	type result struct {
		index int
		rows  []row
	}

	results := make([][]row, len(containers))
	resultCh := make(chan result, len(containers))

	var wg sync.WaitGroup
	for i, c := range containers {
		wg.Add(1)
		go func(index int, container config.ContainerConfig) {
			defer wg.Done()
			resultCh <- result{
				index: index,
				rows:  emulatorCheckRows(ctx, rt, emulatorClient, container, localStackHost),
			}
		}(i, c)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for result := range resultCh {
		results[result.index] = result.rows
	}

	var rows []row
	for _, resultRows := range results {
		rows = append(rows, resultRows...)
	}

	return rows
}

func emulatorCheckRows(ctx context.Context, rt runtime.Runtime, emulatorClient emulator.Client, c config.ContainerConfig, localStackHost string) []row {
	name := c.Name()
	label := c.DisplayName()

	runningCtx, cancel := context.WithTimeout(ctx, emulatorInspectTimeout)
	running, err := rt.IsRunning(runningCtx, name)
	cancel()
	if err != nil {
		if timedOut(runningCtx, err) {
			return []row{{
				check:  label,
				status: statusFail,
				detail: fmt.Sprintf("Checking %s timed out after %s", name, emulatorInspectTimeout),
			}}
		}
		return []row{{
			check:  label,
			status: statusFail,
			detail: fmt.Sprintf("Could not inspect %s: %v", name, err),
		}}
	}
	if !running {
		return []row{{
			check:  label,
			status: statusWarn,
			detail: fmt.Sprintf("%s is not running", name),
		}}
	}

	port := c.Port
	if containerPort, err := c.ContainerPort(); err == nil {
		boundPortCtx, cancel := context.WithTimeout(ctx, emulatorInspectTimeout)
		if actualPort, err := rt.GetBoundPort(boundPortCtx, name, containerPort); err == nil && actualPort != "" {
			port = actualPort
		}
		cancel()
	}
	host, dnsOK := endpoint.ResolveHost(port, localStackHost)
	detail := fmt.Sprintf("%s is running at %s", name, host)
	if localStackHost == "" && !dnsOK {
		detail += " (DNS fallback to 127.0.0.1)"
	}

	rows := []row{{
		check:  label,
		status: statusOK,
		detail: detail,
	}}

	if c.Type != config.EmulatorAWS || emulatorClient == nil {
		return rows
	}

	healthCtx, cancel := context.WithTimeout(ctx, emulatorHealthTimeout)
	version, err := emulatorClient.FetchVersion(healthCtx, host)
	cancel()
	if err != nil {
		if timedOut(healthCtx, err) {
			rows = append(rows, row{
				check:  label + " health",
				status: statusWarn,
				detail: fmt.Sprintf("Health check for %s timed out after %s", host, emulatorHealthTimeout),
			})
			return rows
		}
		rows = append(rows, row{
			check:  label + " health",
			status: statusWarn,
			detail: fmt.Sprintf("Health check failed for %s: %v", host, err),
		})
		return rows
	}

	rows = append(rows, row{
		check:  label + " health",
		status: statusOK,
		detail: fmt.Sprintf("%s · version %s", host, version),
	})
	return rows
}

func dockerDetail(rt runtime.Runtime) string {
	if rt == nil {
		return "Docker client unavailable"
	}
	if socketPath := rt.SocketPath(); socketPath != "" {
		return "Connected via unix://" + socketPath
	}
	return "Connected via Docker host configuration"
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func timedOut(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)
}
