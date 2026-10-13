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
	ConfigPath string // empty for an --endpoint-url target
}

func Set(ctx context.Context, sink output.Sink, t Target, enabled, persist bool) error {
	running, err := isRunning(ctx, sink, t, persist)
	if err != nil {
		return err
	}
	if !running && !persist {
		return emitNotRunning(sink, output.ErrorAction{Label: "Or apply it on the next start:", Value: "lstk inspect " + verb(enabled) + " --persist"})
	}

	if running {
		changed, err := t.Client.SetAppInspectorEnabled(ctx, t.Host, enabled)
		if err != nil {
			return emitRequestError(sink, err)
		}
		sink.Emit(output.AppInspectorEvent{Enabled: enabled, Changed: changed})
	}

	if !persist {
		if enabled && t.ConfigPath != "" {
			sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "To keep it on after restarts, run: lstk inspect enable --persist"})
		}
		return nil
	}

	value := "0"
	if enabled {
		value = "1"
	}
	changed, err := config.SetEnvProfileVar("app-inspector", "APP_INSPECTOR", value)
	if err != nil {
		sink.Emit(output.ErrorEvent{Title: "Could not save the App Inspector setting to " + t.ConfigPath, Summary: err.Error(), Code: output.ErrConfigInvalid})
		return output.NewSilentError(err)
	}
	sink.Emit(output.AppInspectorSavedEvent{Enabled: enabled, Changed: changed, ConfigPath: t.ConfigPath})
	if !running {
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "The emulator is not running. The setting applies on the next start."})
	}
	return nil
}

func Status(ctx context.Context, sink output.Sink, t Target) error {
	running, err := isRunning(ctx, sink, t, false)
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

// With persist, an unreachable Docker counts as "not running": saving needs no emulator.
func isRunning(ctx context.Context, sink output.Sink, t Target, persist bool) (bool, error) {
	if err := t.Runtime.IsHealthy(ctx); err != nil {
		if persist {
			return false, nil
		}
		t.Runtime.EmitUnhealthyError(sink, err)
		return false, output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}
	running, err := container.RunningEmulators(ctx, t.Runtime, t.Containers)
	if err != nil {
		return false, fmt.Errorf("checking emulator status: %w", err)
	}
	return len(running) > 0, nil
}

func emitNotRunning(sink output.Sink, extra ...output.ErrorAction) error {
	sink.Emit(output.ErrorEvent{
		Title:   "LocalStack is not running",
		Actions: append([]output.ErrorAction{{Label: "Start LocalStack:", Value: "lstk"}}, extra...),
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

func verb(enabled bool) string {
	if enabled {
		return "enable"
	}
	return "disable"
}
