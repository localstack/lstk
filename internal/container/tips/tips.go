// Package tips decides the one "> Tip:" line shown after an emulator start.
// Tips are selected at random from a set of possible tips. Some tips are only
// relevant once a detector function has been run, and provides the necessary output.
package tips

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
)

// completionTip fires on first run, not install.
const completionTip = "> Tip: Set up tab completion: lstk completion"

// Tip is one post-start tip: static text, or a detector that decides at
// selection time whether it has anything to say.
type Tip struct {
	// string content of the tip (for static-text tips)
	text string
	// eligible() must be a cheap and side-effect-free function to decide whether
	// the tip is eligible to be displayed right now.
	eligible func(config.EmulatorType) bool
	// detect() may do anything (exec, API calls) but must operate quietly (no
	// side effects, no output, no telemetry). The response from this function
	// is used as the tip text. nil means no detector function.
	detect func(ctx context.Context) (string, bool)
}

func staticTip(text string) Tip { return Tip{text: text} }

func (t Tip) isEligible(emulatorType config.EmulatorType) bool {
	return t.eligible == nil || t.eligible(emulatorType)
}

// Select returns the one tip to show after a start of emulatorType, or "" for
// none (including when nothing was started). Rank a new tip in here rather
// than adding an emit site.
func Select(ctx context.Context, emulatorType config.EmulatorType, firstRun, interactive bool, detectorTips []Tip, logger log.Logger) string {
	return newSelector(logger).selectTip(ctx, emulatorType, firstRun, interactive, detectorTips)
}

type selector struct {
	intn     func(int) int
	deadline time.Duration
	logger   log.Logger
}

func newSelector(logger log.Logger) selector {
	if logger == nil {
		logger = log.Nop()
	}
	return selector{intn: rand.IntN, deadline: detectorDeadline, logger: logger}
}

// select a tip to show the user. On firstRun == true, always show the completion tip.
// Otherwise, pick a random tip from the eligible tips. If a tip has a detector, run it
// and use its output. If the detector fails, pick a random static tip instead.
func (s selector) selectTip(ctx context.Context, emulatorType config.EmulatorType, firstRun, interactive bool, detectorTips []Tip) string {
	if emulatorType == "" { // nothing started, nothing to tip about
		return ""
	}
	// Interactive only: completion means nothing to CI, agents, or --json.
	if firstRun && interactive {
		return completionTip
	}
	var candidates []Tip
	for _, t := range append(rotatingTips(emulatorType), detectorTips...) {
		if t.isEligible(emulatorType) {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	picked := candidates[s.intn(len(candidates))]
	if picked.detect == nil {
		return picked.text
	}
	if text, ok := s.runDetector(ctx, picked); ok {
		return text
	}
	var statics []Tip
	for _, t := range candidates {
		if t.detect == nil {
			statics = append(statics, t)
		}
	}
	if len(statics) == 0 {
		return ""
	}
	return statics[s.intn(len(statics))].text
}

func rotatingTips(t config.EmulatorType) []Tip {
	var texts []string
	switch t {
	case config.EmulatorAWS:
		texts = []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: View deployed resources: lstk status",
		}
	case config.EmulatorSnowflake:
		texts = []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: Check emulator status: lstk status",
		}
	case config.EmulatorAzure:
		texts = []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: Check emulator status: lstk status",
		}
	}
	tips := make([]Tip, 0, len(texts))
	for _, text := range texts {
		tips = append(tips, staticTip(text))
	}
	return tips
}
