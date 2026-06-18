package snapshot

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
)

// emitExperimentalWarning warns the user when a snapshot operation targets a
// non-AWS emulator, whose snapshot support is not yet fully tested. The AWS
// emulator is the well-tested path and produces no warning.
func emitExperimentalWarning(containers []config.ContainerConfig, sink output.Sink) {
	for _, c := range containers {
		if c.Type == config.EmulatorAWS {
			continue
		}
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityWarning,
			Text:     fmt.Sprintf("Snapshot support for the %s emulator is experimental and not fully tested.", c.Type.ShortName()),
		})
	}
}
