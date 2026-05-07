package container

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
)

func SelectEmulator(
	ctx context.Context,
	sink output.Sink,
	configPath string,
) ([]config.ContainerConfig, error) {
	responseCh := make(chan output.InputResponse, 1)
	sink.Emit(output.UserInputRequestEvent{
		Prompt: "Which emulator would you like to use?",
		Options: []output.InputOption{
			{Key: "a", Label: "AWS"},
			{Key: "s", Label: "Snowflake"},
		},
		ResponseCh: responseCh,
		Vertical:   true,
	})

	var resp output.InputResponse
	select {
	case resp = <-responseCh:
	case <-ctx.Done():
		return nil, context.Canceled
	}

	if resp.Cancelled {
		return nil, context.Canceled
	}

	selected := config.EmulatorAWS
	if resp.SelectedKey == "s" {
		selected = config.EmulatorSnowflake
	}

	if err := config.SetEmulatorType(selected); err != nil {
		return nil, fmt.Errorf("failed to set emulator type: %w", err)
	}
	newCfg, err := config.Get()
	if err != nil {
		return nil, err
	}

	sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: selected.DisplayName() + " emulator selected."})
	if configPath != "" {
		sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: "Change configuration in " + configPath + "."})
	}

	return newCfg.Containers, nil
}
