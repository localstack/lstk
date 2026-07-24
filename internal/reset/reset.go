package reset

//go:generate mockgen -source=reset.go -destination=mock_state_resetter_test.go -package=reset_test

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// StateResetter clears state in the running LocalStack instance.
type StateResetter interface {
	ResetState(ctx context.Context, host string) error
}

func Reset(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, resetter StateResetter, host string, force bool, sink output.Sink) (retErr error) {
	target, resolved, err := container.FirstReachableEmulator(ctx, rt, sink, containers, host)
	if err != nil {
		return err
	}
	if !resolved.Found() {
		sink.Emit(output.ErrorEvent{
			Title: "LocalStack is not running",
			Actions: []output.ErrorAction{
				{Label: "Start LocalStack:", Value: "lstk"},
				{Label: "See help:", Value: "lstk -h"},
			},
			Code: output.ErrEmulatorNotRunning,
		})
		return output.NewSilentError(fmt.Errorf("LocalStack is not running"))
	}

	if !force {
		responseCh := make(chan output.InputResponse, 1)
		sink.Emit(output.UserInputRequestEvent{
			Prompt: "Reset emulator state? All resources will be lost",
			Options: []output.InputOption{
				{Key: "y", Label: "Yes"},
				{Key: "n", Label: "NO"},
			},
			ResponseCh: responseCh,
		})

		select {
		case resp := <-responseCh:
			if resp.Cancelled || resp.SelectedKey != "y" {
				sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Cancelled"})
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	sink.Emit(output.SpinnerStart("Resetting state..."))
	defer func() {
		sink.Emit(output.SpinnerStop())
		if retErr == nil {
			sink.Emit(output.EmulatorResetEvent{Type: string(target.Type), Name: target.Name()})
		}
	}()

	if err := resetter.ResetState(ctx, host); err != nil {
		return fmt.Errorf("reset state: %w", err)
	}
	return nil
}
