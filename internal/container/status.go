package container

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator"
	"github.com/localstack/lstk/internal/emulator/snowflake"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

const statusTimeout = 10 * time.Second

func Status(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, localStackHost string, clients map[config.EmulatorType]emulator.Client, sink output.Sink) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	for _, c := range containers {
		name, err := ResolveRunningContainerName(ctx, rt, c)
		if err != nil {
			return fmt.Errorf("checking %s running: %w", c.Name(), err)
		}
		if name == "" {
			return HandleNoRunningContainer(sink, c)
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
		host, _ := endpoint.ResolveHost(ctx, port, localStackHost)
		if c.Type == config.EmulatorSnowflake {
			if h := snowflake.Hostname(host); h != "" {
				host = h
			}
		}

		var uptime time.Duration
		if startedAt, err := rt.ContainerStartedAt(ctx, name); err == nil {
			uptime = time.Since(startedAt)
		}

		var version string
		var rows []emulator.Resource
		if client, ok := clients[c.Type]; ok {
			baseURL := "http://" + host
			sink.Emit(output.SpinnerStart("Fetching LocalStack status"))
			if v, err := client.FetchVersion(ctx, baseURL); err != nil {
				sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Could not fetch version: %v", err)})
			} else {
				version = v
			}

			var fetchErr error
			rows, fetchErr = client.FetchResources(ctx, baseURL)
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
			Persistence:   c.Type == config.EmulatorAWS && isPersistenceEnabled(ctx, rt, name),
		})

		if c.Type == config.EmulatorAWS {
			emitResources(sink, rows)
		}
	}

	return nil
}

// StatusExternal renders status for an externally-managed endpoint
// (--endpoint-url/LSTK_ENDPOINT_URL/AWS_ENDPOINT_URL): reachability, detected
// type, reported version, and — for an AWS-typed target — deployed resources,
// without the Docker-derived facts (container name, uptime, persistence, bound
// port) that don't exist for an emulator lstk didn't start. Deployed resources
// are not Docker-derived (they're an ordinary emulator API call via
// FetchResources, identical to Status above), so there's no reason to omit
// them here. It emits the same events in the same order as Status, so both
// paths render identically through whichever sink the caller picked.
func StatusExternal(ctx context.Context, target *endpoint.Target, clients map[config.EmulatorType]emulator.Client, sink output.Sink) error {
	var version string
	var rows []emulator.Resource
	if client, ok := clients[target.Type]; ok {
		sink.Emit(output.SpinnerStart("Fetching LocalStack status"))
		if v, err := client.FetchVersion(ctx, target.URL); err != nil {
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Could not fetch version: %v", err)})
		} else {
			version = v
		}

		var fetchErr error
		rows, fetchErr = client.FetchResources(ctx, target.URL)
		sink.Emit(output.SpinnerStop())
		if fetchErr != nil {
			return fetchErr
		}
	}

	sink.Emit(output.InstanceInfoEvent{
		EmulatorName: target.Type.DisplayName(),
		Version:      version,
		Host:         target.URL,
	})

	if target.Type == config.EmulatorAWS {
		emitResources(sink, rows)
	}

	return nil
}

// StatusJSON reports every configured emulator's running/health state, one
// EmulatorStatusEvent per container — unlike Status above, it doesn't stop
// at the first non-running one. Resources are only fetched when
// includeResources is set, since this path is also polled for readiness.
func StatusJSON(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, localStackHost string, clients map[config.EmulatorType]emulator.Client, sink output.Sink, includeResources bool) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	for _, c := range containers {
		name, err := ResolveRunningContainerName(ctx, rt, c)
		if err != nil {
			return fmt.Errorf("checking %s running: %w", c.Name(), err)
		}
		if name == "" {
			sink.Emit(output.EmulatorStatusEvent{Type: string(c.Type), Running: false})
			continue
		}

		port := c.Port
		if containerPort, err := c.ContainerPort(); err == nil {
			if actualPort, err := rt.GetBoundPort(ctx, name, containerPort); err == nil {
				port = actualPort
			}
		}
		host, _ := endpoint.ResolveHost(ctx, port, localStackHost)
		if c.Type == config.EmulatorSnowflake {
			if h := snowflake.Hostname(host); h != "" {
				host = h
			}
		}

		var uptime time.Duration
		if startedAt, err := rt.ContainerStartedAt(ctx, name); err == nil {
			uptime = time.Since(startedAt)
		}

		event := output.EmulatorStatusEvent{
			Type:          string(c.Type),
			Running:       true,
			Name:          name,
			Host:          host,
			UptimeSeconds: int64(uptime.Seconds()),
			Persistence:   c.Type == config.EmulatorAWS && isPersistenceEnabled(ctx, rt, name),
		}

		if client, ok := clients[c.Type]; ok {
			baseURL := "http://" + host
			if v, err := client.FetchVersion(ctx, baseURL); err != nil {
				event.Health = "unhealthy"
			} else {
				event.Health = "healthy"
				event.Version = v
			}

			if includeResources && c.Type == config.EmulatorAWS {
				rows, err := client.FetchResources(ctx, baseURL)
				if err != nil {
					return err
				}
				event.Resources = make([]output.EmulatorStatusResource, len(rows))
				for i, r := range rows {
					event.Resources[i] = output.EmulatorStatusResource{Service: r.Service, Name: r.Name, Region: r.Region, Account: r.Account}
				}
			}
		}

		sink.Emit(event)
	}

	return nil
}

func emitResources(sink output.Sink, rows []emulator.Resource) {
	if len(rows) == 0 {
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "No resources deployed"})
		return
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
