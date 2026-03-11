package container

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator/aws"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

const statusTimeout = 10 * time.Second

func Status(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, localStackHost string, sink output.Sink) error {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	for _, c := range containers {
		name := c.Name()
		running, err := rt.IsRunning(ctx, name)
		if err != nil {
			return fmt.Errorf("checking %s running: %w", name, err)
		}
		if !running {
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
		switch c.Type {
		case config.EmulatorAWS:
			emulatorClient := aws.NewClient(nil)
			if v, err := emulatorClient.FetchVersion(ctx, host); err != nil {
				output.EmitWarning(sink, fmt.Sprintf("Could not fetch version: %v", err))
			} else {
				version = v
			}

			output.Emit(sink, output.InstanceInfoEvent{
				EmulatorName:  c.DisplayName(),
				Version:       version,
				Host:          host,
				ContainerName: name,
				Uptime:        uptime,
			})

			rows, err := emulatorClient.FetchResources(ctx, host)
			if err != nil {
				return err
			}

			if len(rows) == 0 {
				output.EmitNote(sink, "No resources deployed")
				continue
			}

			services := map[string]struct{}{}
			for _, r := range rows {
				services[r.Service] = struct{}{}
			}
			output.Emit(sink, output.ResourceSummaryEvent{
				ResourceCount: len(rows),
				ServiceCount:  len(services),
			})
			output.Emit(sink, output.ResourceTableEvent{Rows: rows})
		default:
			output.Emit(sink, output.InstanceInfoEvent{
				EmulatorName:  c.DisplayName(),
				Version:       version,
				Host:          host,
				ContainerName: name,
				Uptime:        uptime,
			})
		}
	}

	return nil
}
