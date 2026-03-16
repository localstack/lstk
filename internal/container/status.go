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
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	output.EmitSpinnerStart(sink, "Fetching LocalStack status")

	for _, c := range containers {
		name := c.Name()
		running, err := rt.IsRunning(ctx, name)
		if err != nil {
			output.EmitSpinnerStop(sink)
			return fmt.Errorf("checking %s running: %w", name, err)
		}
		if !running {
			output.EmitSpinnerStop(sink)
			output.EmitError(sink, output.ErrorEvent{
				Title: fmt.Sprintf("%s is not running", c.DisplayName()),
				Actions: []output.ErrorAction{
					{Label: "Start LocalStack:", Value: "lstk"},
					{Label: "See help:", Value: "lstk -h"},
				},
			})
			return output.NewSilentError(fmt.Errorf("%s is not running", name))
		}

		host, _ := endpoint.ResolveHost(c.Port, localStackHost)

		var uptime time.Duration
		if startedAt, err := rt.ContainerStartedAt(ctx, name); err == nil {
			uptime = time.Since(startedAt)
		}

		var version string
		var rows []emulator.Resource
		switch c.Type {
		case config.EmulatorAWS:
			if v, err := emulatorClient.FetchVersion(ctx, host); err != nil {
				output.EmitSpinnerStop(sink)
				output.EmitWarning(sink, fmt.Sprintf("Could not fetch version: %v", err))
			} else {
				version = v
			}

			var fetchErr error
			rows, fetchErr = emulatorClient.FetchResources(ctx, host)
			if fetchErr != nil {
				output.EmitSpinnerStop(sink)
				return fetchErr
			}
		}

		output.EmitSpinnerStop(sink)

		output.EmitInstanceInfo(sink, output.InstanceInfoEvent{
			EmulatorName:  c.DisplayName(),
			Version:       version,
			Host:          host,
			ContainerName: name,
			Uptime:        uptime,
		})

		if c.Type == config.EmulatorAWS {
			if len(rows) == 0 {
				output.EmitNote(sink, "No resources deployed")
				continue
			}

			tableRows := make([][]string, len(rows))
			services := map[string]struct{}{}
			for i, r := range rows {
				tableRows[i] = []string{r.Service, r.Name, r.Region, r.Account}
				services[r.Service] = struct{}{}
			}

			output.EmitResourceSummary(sink, len(rows), len(services))
			output.EmitTable(sink, output.TableEvent{
				Headers: []string{"Service", "Resource", "Region", "Account"},
				Rows:    tableRows,
			})
		}
	}

	return nil
}
