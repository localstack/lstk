package container

import (
	"math/rand/v2"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
)

// completionTip fires on first run, not install: no install path has a usable
// hook (npm's package.json is generated, binaries have none). It names the bare
// command rather than a URL — `lstk completion` carries the per-shell setup
// itself (completionShells in cmd/completion.go), so there is nothing to follow
// and nothing to keep in sync. Must stay a plain MessageEvent — ui.Run renders
// no DeferredOutput, so a deferred event is lost.
const completionTip = "> Tip: Set up tab completion: lstk completion"

// emitPostStartTip emits this run's tip. Start is its only caller: one emit site
// is what makes selectTip's limit hold.
func emitPostStartTip(sink output.Sink, emulatorType config.EmulatorType, firstRun, interactive bool) {
	// Nothing came up (no containers configured), so nothing to tip about.
	if emulatorType == "" {
		return
	}
	if tip := selectTip(emulatorType, firstRun, interactive); tip != "" {
		sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: tip})
	}
}

// selectTip returns the one tip to show after a start, or "" for none.
//
// One tip per run, never two: side by side they compete and neither lands (#484
// review). Rank a new tip in here, don't emit it separately. firstRun wins — it
// happens once per install, the rotating tips return on every later start.
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
