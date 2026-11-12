package inspect

import (
	"context"
	"errors"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

var ErrUnavailable = errors.New("this emulator has no App Inspector")

type Client interface {
	AppInspectorEnabled(ctx context.Context, host string) (bool, error)
	SetAppInspectorEnabled(ctx context.Context, host string, enabled bool) (changed bool, err error)
}

type Target struct {
	Runtime    runtime.Runtime
	Containers []config.ContainerConfig
	Client     Client
	Host       string
}

func Set(ctx context.Context, sink output.Sink, t Target, enabled bool) error {
	running, err := isRunning(ctx, sink, t)
	if err != nil {
		return err
	}
	if !running {
		return emitNotRunning(sink)
	}
	changed, err := t.Client.SetAppInspectorEnabled(ctx, t.Host, enabled)
	if err != nil {
		return emitRequestError(sink, err)
	}
	sink.Emit(output.AppInspectorEvent{Enabled: enabled, Changed: changed})
	return nil
}

func Status(ctx context.Context, sink output.Sink, t Target) error {
	running, err := isRunning(ctx, sink, t)
	if err != nil {
		return err
	}
	if !running {
		return emitNotRunning(sink)
	}
	enabled, err := t.Client.AppInspectorEnabled(ctx, t.Host)
	if err != nil {
		return emitRequestError(sink, err)
	}
	sink.Emit(output.AppInspectorEvent{Enabled: enabled})
	return nil
}

func isRunning(ctx context.Context, sink output.Sink, t Target) (bool, error) {
	if err := t.Runtime.IsHealthy(ctx); err != nil {
		t.Runtime.EmitUnhealthyError(sink, err)
		return false, output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}
	running, err := container.RunningEmulators(ctx, t.Runtime, t.Containers)
	if err != nil {
		return false, fmt.Errorf("checking emulator status: %w", err)
	}
	return len(running) > 0, nil
}

func emitNotRunning(sink output.Sink) error {
	sink.Emit(output.ErrorEvent{
		Title:   "LocalStack is not running",
		Actions: []output.ErrorAction{{Label: "Start LocalStack:", Value: "lstk"}},
		Code:    output.ErrEmulatorNotRunning,
	})
	return output.NewSilentError(errors.New("LocalStack is not running"))
}

func emitRequestError(sink output.Sink, err error) error {
	if errors.Is(err, ErrUnavailable) {
		sink.Emit(output.ErrorEvent{Title: "App Inspector is not available in this emulator", Summary: "Use a recent LocalStack image."})
	} else {
		sink.Emit(output.ErrorEvent{Title: "App Inspector request failed", Summary: err.Error()})
	}
	return output.NewSilentError(err)
}
