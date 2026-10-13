package inspect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// ErrUnavailable means the emulator has no App Inspector, e.g. an image that
// predates it.
var ErrUnavailable = errors.New("this emulator has no App Inspector")

const (
	// profileName is the [env.*] profile lstk owns for the saved setting.
	profileName = "app-inspector"
	// envVar turns App Inspector on at emulator startup. The runtime switch
	// does not survive a restart.
	envVar = "APP_INSPECTOR"
)

// Client reads and flips App Inspector on a running AWS emulator.
type Client interface {
	AppInspectorEnabled(ctx context.Context, host string) (bool, error)
	SetAppInspectorEnabled(ctx context.Context, host string, enabled bool) (changed bool, err error)
}

// Target is the emulator a command acts on.
type Target struct {
	Runtime    runtime.Runtime
	Containers []config.ContainerConfig
	Client     Client
	Host       string
	// Config is nil for an --endpoint-url target: lstk does not start that
	// emulator, so its config says nothing about it.
	Config *LocalConfig
}

// LocalConfig is the part of the lstk config that decides App Inspector's
// state when lstk starts the emulator.
type LocalConfig struct {
	Container config.ContainerConfig
	Env       map[string]map[string]string
	Path      string
}

// Set enables or disables App Inspector on the running emulator. With persist
// it also saves the setting to the config file, which works while the
// emulator is stopped too.
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
		if changed {
			sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: "App Inspector " + state(enabled)})
		} else {
			sink.Emit(output.MessageEvent{Severity: output.SeverityInfo, Text: "App Inspector is already " + state(enabled)})
		}
	}

	if persist {
		if err := save(sink, t.Config, enabled); err != nil {
			return err
		}
		if !running {
			sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "The emulator is not running. The setting applies on the next start."})
		}
		return nil
	}
	emitRestartNote(sink, t.Config, enabled)
	return nil
}

// Status reports whether App Inspector is enabled on the running emulator.
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
	sink.Emit(output.MessageEvent{Severity: output.SeverityInfo, Text: "App Inspector is " + state(enabled)})
	emitRestartNote(sink, t.Config, enabled)
	return nil
}

// isRunning treats an unreachable Docker as "not running" when persisting,
// since saving the setting does not need the emulator.
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

func save(sink output.Sink, cfg *LocalConfig, enabled bool) error {
	if cfg == nil {
		return errors.New("no lstk config to save the App Inspector setting to")
	}
	fail := func(err error) error {
		sink.Emit(output.ErrorEvent{Title: "Could not save the App Inspector setting to " + cfg.Path, Summary: err.Error()})
		return output.NewSilentError(err)
	}
	saved, err := enabledOnStart(cfg)
	if err != nil {
		return fail(err)
	}
	if saved == enabled {
		sink.Emit(output.MessageEvent{Severity: output.SeverityInfo, Text: fmt.Sprintf("App Inspector is already %s on every start (%s)", state(enabled), cfg.Path)})
		return nil
	}
	value := "0"
	if enabled {
		value = "1"
	}
	if err := config.SetEnvProfileVar(profileName, envVar, value); err != nil {
		return fail(err)
	}
	sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("App Inspector will be %s on every start (saved to %s)", state(enabled), cfg.Path)})
	return nil
}

// emitRestartNote warns when the running state will not survive a restart,
// because the config says otherwise.
func emitRestartNote(sink output.Sink, cfg *LocalConfig, enabled bool) {
	if cfg == nil {
		return
	}
	onStart, err := enabledOnStart(cfg)
	if err != nil || onStart == enabled {
		return
	}
	text := "App Inspector turns off when the emulator restarts. To keep it on, run: lstk inspect enable --persist"
	if !enabled {
		text = fmt.Sprintf("App Inspector turns back on when the emulator restarts, as %s enables it. To keep it off, run: lstk inspect disable --persist", cfg.Path)
	}
	sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: text})
}

// enabledOnStart reports whether the config's env profiles turn App Inspector
// on at startup. The last profile setting the variable wins, and LocalStack
// reads "1" and "true" as on.
func enabledOnStart(cfg *LocalConfig) (bool, error) {
	env, err := cfg.Container.ResolvedEnv(cfg.Env)
	if err != nil {
		return false, err
	}
	enabled := false
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == envVar {
			v = strings.ToLower(strings.TrimSpace(v))
			enabled = v == "1" || v == "true"
		}
	}
	return enabled, nil
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
		sink.Emit(output.ErrorEvent{
			Title:   "App Inspector is not available in this emulator",
			Summary: "This emulator version predates App Inspector. Use a recent LocalStack image.",
		})
		return output.NewSilentError(err)
	}
	sink.Emit(output.ErrorEvent{Title: "App Inspector request failed", Summary: err.Error()})
	return output.NewSilentError(err)
}

func state(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func verb(enabled bool) string {
	if enabled {
		return "enable"
	}
	return "disable"
}
