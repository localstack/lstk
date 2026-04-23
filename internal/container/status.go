package container

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

const statusTimeout = 10 * time.Second

func Status(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, localStackHost string, emulatorClient emulator.Client, sink output.Sink) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	for _, c := range containers {
		name := c.Name()
		running, err := rt.IsRunning(ctx, name)
		if err != nil {
			return fmt.Errorf("checking %s running: %w", name, err)
		}
		if !running {
			sink.Emit(output.ErrorEvent{
				Title: fmt.Sprintf("%s is not running", c.DisplayName()),
				Actions: []output.ErrorAction{
					{Label: "Start LocalStack:", Value: "lstk"},
					{Label: "See help:", Value: "lstk -h"},
				},
			})
			return output.NewSilentError(fmt.Errorf("%s is not running", name))
		}

		// status makes direct HTTP calls to LocalStack, so it needs the actual host port.
		// Ask Docker rather than trusting the config: the user may have changed the config
		// port while the container still runs on the old one.
		port := c.Port
		if containerPort, err := c.ContainerPort(); err == nil {
			if actualPort, err := rt.GetBoundPort(ctx, name, containerPort); err == nil {
				port = actualPort
			}
		}
		host, _ := endpoint.ResolveHost(port, localStackHost)

		var uptime time.Duration
		if startedAt, err := rt.ContainerStartedAt(ctx, name); err == nil {
			uptime = time.Since(startedAt)
		}

		var version string
		var rows []emulator.Resource
		switch c.Type {
		case config.EmulatorAWS:
			sink.Emit(output.SpinnerStart("Fetching LocalStack status"))
			if v, err := emulatorClient.FetchVersion(ctx, host); err != nil {
				sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Could not fetch version: %v", err)})
			} else {
				version = v
			}

			var fetchErr error
			rows, fetchErr = emulatorClient.FetchResources(ctx, host)
			sink.Emit(output.SpinnerStop())
			if fetchErr != nil {
				return fetchErr
			}
		}

		sink.Emit(output.InstanceInfoEvent{
			EmulatorName:  c.DisplayName(),
			Version:       version,
			Host:          host,
			ContainerName: name,
			Uptime:        uptime,
		})

		if c.Type == config.EmulatorAWS {
			if len(rows) == 0 {
				sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "No resources deployed"})
				continue
			}

			tableRows := make([][]string, len(rows))
			services := map[string]struct{}{}
			for i, r := range rows {
				tableRows[i] = []string{r.Service, r.Name, r.Region, r.Account}
				services[r.Service] = struct{}{}
			}

			sink.Emit(output.ResourceSummaryEvent{Resources: len(rows), Services: len(services)})
			sink.Emit(output.TableEvent{
				Headers: []string{"Service", "Resource", "Region", "Account"},
				Rows:    tableRows,
			})
		}
	}

	return nil
}
