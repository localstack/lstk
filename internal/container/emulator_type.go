package container

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
)

// ApplyEmulatorType applies a non-interactive emulator selection (the --type
// flag) to the config before start, returning the resulting containers.
//
// It is the scripted counterpart to the interactive picker (SelectEmulator):
//   - First run (no config file yet): create the config and record the requested
//     type, reusing the same path the picker writes.
//   - Config already matches: no-op.
//   - Config differs: switch the type in place via the surgical type-line rewrite
//     (comments/formatting preserved), keeping the other block fields. A custom
//     image is a hard error — it pins a specific product that cannot be
//     reinterpreted under a different emulator type. A non-default tag or any
//     volume mounts are kept with a warning, since they are often product-specific.
//
// Messages are emitted through sink; configPath is the friendly config path used
// in those messages so a switch against a checked-in file is visible.
func ApplyEmulatorType(sink output.Sink, requested config.EmulatorType, containers []config.ContainerConfig, firstRun bool, configPath string) ([]config.ContainerConfig, error) {
	if firstRun {
		if err := config.EnsureCreated(); err != nil {
			return nil, fmt.Errorf("failed to create config file: %w", err)
		}
		if err := config.SetEmulatorType(requested); err != nil {
			return nil, fmt.Errorf("failed to set emulator type: %w", err)
		}
		newCfg, err := config.Get()
		if err != nil {
			return nil, err
		}
		sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: requested.ShortName() + " emulator selected."})
		if configPath != "" {
			sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: "Change configuration in " + configPath + "."})
		}
		return newCfg.Containers, nil
	}

	// A config that exists but has no [[containers]] block has no emulator to
	// select (and would panic the containers[0] access below); surface a clear,
	// actionable error instead of the raw rewrite failure.
	if len(containers) == 0 {
		err := fmt.Errorf("config has no [[containers]] block")
		sink.Emit(output.ErrorEvent{
			Title:   "Incomplete configuration",
			Summary: "The config file has no [[containers]] block, so there is no emulator to select.",
			Actions: []output.ErrorAction{{Label: "Add a [[containers]] block, or delete the file to regenerate it:", Value: "lstk config path"}},
		})
		return nil, output.NewSilentError(err)
	}

	// Reject a multi-block config before touching the file: only one block can
	// start (checkSingleContainer enforces this on the start path), and rewriting
	// one block's type while the start is doomed to fail would leave a confusing
	// half-changed config.
	if err := checkSingleContainer(containers); err != nil {
		sink.Emit(output.ErrorEvent{
			Title:   "Unsupported configuration",
			Summary: err.Error(),
			Actions: []output.ErrorAction{{Label: "Edit your config file so only one [[containers]] block is enabled:", Value: "lstk config path"}},
		})
		return nil, output.NewSilentError(err)
	}

	current := containers[0]
	if current.Type == requested {
		return containers, nil
	}

	// configPath can be empty when the friendly path couldn't be resolved; fall
	// back to a generic phrase so the messages below still read as sentences.
	location := configPath
	if location == "" {
		location = "your config file"
	}

	if current.CustomImage != "" {
		sink.Emit(output.ErrorEvent{
			Title:   fmt.Sprintf("Cannot switch emulator to %s while a custom image is set", requested.ShortName()),
			Summary: "A custom image pins a specific product, so lstk would run the previous product's image under the new emulator type and health checks.",
			Actions: []output.ErrorAction{
				{Label: "Remove or update 'image' in", Value: location},
				{Label: "Or keep a separate profile with", Value: "lstk start --type " + string(requested) + " --config <path>"},
			},
		})
		return nil, output.NewSilentError(fmt.Errorf("cannot switch emulator type while a custom image is set"))
	}

	if current.Tag != "" && current.Tag != "latest" {
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityWarning,
			Text:     fmt.Sprintf("Keeping tag %q, which may not exist for the %s emulator — update it in %s if the start fails.", current.Tag, requested.ShortName(), location),
		})
	}
	if current.Volume != "" || len(current.Volumes) > 0 {
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityWarning,
			Text:     fmt.Sprintf("Keeping volume mounts, which are now shared with the %s emulator and may be product-specific — review them in %s.", requested.ShortName(), location),
		})
	}

	if err := config.SetEmulatorType(requested); err != nil {
		return nil, fmt.Errorf("failed to switch emulator type: %w", err)
	}
	newCfg, err := config.Get()
	if err != nil {
		return nil, err
	}
	note := fmt.Sprintf("Switched configured emulator to %s", requested.ShortName())
	if configPath != "" {
		note += fmt.Sprintf(" (%s)", configPath)
	}
	sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: note + "."})
	return newCfg.Containers, nil
}
