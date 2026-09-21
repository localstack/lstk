package container

import (
	"math/rand/v2"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
)

// completionTip fires on first run, not install: no install path offers a hook
// (npm's package.json is generated, binaries have none).
const completionTip = "> Tip: Set up tab completion: lstk completion"

// emitPostStartTip must stay Start's only emit site: that is what holds
// selectTip's one-tip-per-run limit. Plain MessageEvent, not DeferredEvent —
// ui.Run renders no DeferredOutput.
func emitPostStartTip(sink output.Sink, emulatorType config.EmulatorType, firstRun, interactive bool) {
	if emulatorType == "" { // nothing started, nothing to tip about
		return
	}
	if tip := selectTip(emulatorType, firstRun, interactive); tip != "" {
		sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: tip})
	}
}

// selectTip returns the one tip to show after a start, or "" for none: two
// side by side compete and neither lands (#484). Rank a new tip in here rather
// than adding an emit site; firstRun outranks the rotating tips.
func selectTip(emulatorType config.EmulatorType, firstRun, interactive bool) string {
	// Interactive only: completion means nothing to CI, agents, or --json.
	if firstRun && interactive {
		return completionTip
	}
	tips := tipsForType(emulatorType)
	if len(tips) == 0 {
		return ""
	}
	return tips[rand.IntN(len(tips))]
}

func tipsForType(t config.EmulatorType) []string {
	switch t {
	case config.EmulatorAWS:
		return []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: View deployed resources: lstk status",
		}
	case config.EmulatorSnowflake:
		return []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: Check emulator status: lstk status",
		}
	case config.EmulatorAzure:
		return []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: Check emulator status: lstk status",
		}
	}
	return nil
}
