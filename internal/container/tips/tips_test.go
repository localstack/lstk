package tips

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
	"github.com/stretchr/testify/assert"
)

func selectTip(emulatorType config.EmulatorType, firstRun, interactive bool) string {
	return newSelector(log.Nop()).selectTip(context.Background(), emulatorType, firstRun, interactive, nil)
}

func rotatingTexts(t config.EmulatorType) []string {
	var texts []string
	for _, tip := range rotatingTips(t) {
		texts = append(texts, tip.text)
	}
	return texts
}

// pickSequence returns an intn that yields picks in order, repeating the last.
func pickSequence(picks ...int) func(int) int {
	i := 0
	return func(n int) int {
		p := picks[min(i, len(picks)-1)]
		i++
		if p >= n {
			return n - 1
		}
		return p
	}
}

func testSelector(picks ...int) selector {
	return selector{intn: pickSequence(picks...), deadline: detectorDeadline, logger: log.Nop()}
}

// stubDetectorTip counts its detector calls and answers with text/ok.
func stubDetectorTip(calls *atomic.Int32, text string, ok bool) Tip {
	return Tip{detect: func(context.Context) (string, bool) {
		calls.Add(1)
		return text, ok
	}}
}

// A URL in terminal output cannot be clicked and drifts from the shipped binary.
func TestCompletionTip_NamesTheCommandWithoutAURL(t *testing.T) {
	assert.Contains(t, completionTip, "lstk completion")
	assert.NotContains(t, completionTip, "http")
}

func TestSelectTip_FirstRunInteractive_PrefersCompletionTip(t *testing.T) {
	got := selectTip(config.EmulatorAWS, true, true)

	assert.Equal(t, completionTip, got, "first run is the only moment the completion pointer is worth a line")
}

func TestSelectTip_FirstRunNonInteractive_FallsBackToRotatingTip(t *testing.T) {
	got := selectTip(config.EmulatorAWS, true, false)

	assert.NotEqual(t, completionTip, got, "shell completion is irrelevant to CI and agents")
	assert.Contains(t, rotatingTexts(config.EmulatorAWS), got)
}

func TestSelectTip_SubsequentRun_ReturnsRotatingTip(t *testing.T) {
	for range 20 {
		got := selectTip(config.EmulatorAWS, false, true)

		assert.NotEqual(t, completionTip, got, "the completion tip must not repeat past the first run")
		assert.Contains(t, rotatingTexts(config.EmulatorAWS), got)
	}
}

func TestSelectTip_UnknownEmulator_ReturnsNoTip(t *testing.T) {
	assert.Empty(t, selectTip(config.EmulatorType("other"), false, true))
}

func TestSelectTip_UnknownEmulatorOnFirstRun_StillReturnsCompletionTip(t *testing.T) {
	assert.Equal(t, completionTip, selectTip(config.EmulatorType("other"), true, true),
		"the completion tip is about lstk itself, not the emulator that happens to be configured")
}

func TestSelectTip_StaticPickRunsNoDetector(t *testing.T) {
	var calls atomic.Int32
	got := testSelector(0).selectTip(t.Context(), config.EmulatorAWS, false, false, []Tip{stubDetectorTip(&calls, "> Tip: detected", true)})

	assert.Equal(t, rotatingTexts(config.EmulatorAWS)[0], got)
	assert.Zero(t, calls.Load(), "a detector runs only when its tip is picked")
}

func TestSelectTip_DetectorPickedAndReports(t *testing.T) {
	var calls atomic.Int32
	// Index 2 is the detector tip, after the two static AWS tips.
	got := testSelector(2).selectTip(t.Context(), config.EmulatorAWS, false, false, []Tip{stubDetectorTip(&calls, "> Tip: detected", true)})

	assert.Equal(t, "> Tip: detected", got)
	assert.Equal(t, int32(1), calls.Load())
}

func TestSelectTip_DeclinedDetectorFallsBackToStaticOnly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		detect func(context.Context) (string, bool)
	}{
		{"declines", func(context.Context) (string, bool) { return "", false }},
		{"ok without text", func(context.Context) (string, bool) { return "", true }},
		{"panics", func(context.Context) (string, bool) { panic("boom") }},
		{"ignores the deadline", func(context.Context) (string, bool) {
			time.Sleep(time.Minute)
			return "> Tip: too late", true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var first, second atomic.Int32
			chosen := Tip{detect: func(ctx context.Context) (string, bool) {
				first.Add(1)
				return tc.detect(ctx)
			}}
			sel := testSelector(2, 3) // pick the first detector; the fallback pick then overshoots to the last static tip
			sel.deadline = 50 * time.Millisecond

			got := sel.selectTip(t.Context(), config.EmulatorAWS, false, false, []Tip{chosen, stubDetectorTip(&second, "> Tip: second", true)})

			assert.Contains(t, rotatingTexts(config.EmulatorAWS), got, "the fallback is a static tip from the same tier")
			assert.Equal(t, int32(1), first.Load())
			assert.Zero(t, second.Load(), "at most one detector runs per start")
		})
	}
}

func TestSelectTip_IneligibleTipIsNeverPicked(t *testing.T) {
	var calls atomic.Int32
	ineligible := stubDetectorTip(&calls, "> Tip: detected", true)
	ineligible.eligible = func(config.EmulatorType) bool { return false }

	for pick := range 3 {
		got := testSelector(pick).selectTip(t.Context(), config.EmulatorAWS, false, false, []Tip{ineligible})
		assert.Contains(t, rotatingTexts(config.EmulatorAWS), got)
	}
	assert.Zero(t, calls.Load())
}

func TestSelectTip_TierWithOnlyADecliningDetectorShowsNoTip(t *testing.T) {
	var calls atomic.Int32
	got := testSelector(0).selectTip(t.Context(), "other", false, false, []Tip{stubDetectorTip(&calls, "", false)})

	assert.Empty(t, got)
	assert.Equal(t, int32(1), calls.Load())
}

func TestSelectTip_NothingStarted_ReturnsNoTip(t *testing.T) {
	assert.Empty(t, selectTip("", true, true))
}

// Moved from container's emit test: a detector, reporting or declining,
// still yields exactly one tip line.
func TestSelect_DetectorTipsYieldExactlyOneTip(t *testing.T) {
	var calls atomic.Int32
	for name, detectors := range map[string][]Tip{
		"reporting detectors": {stubDetectorTip(&calls, "> Tip: detected", true), stubDetectorTip(&calls, "> Tip: detected too", true)},
		"declining detector":  {stubDetectorTip(&calls, "", false)},
	} {
		t.Run(name, func(t *testing.T) {
			for range 10 {
				got := Select(t.Context(), config.EmulatorAWS, false, false, detectors, log.Nop())

				assert.Equal(t, 1, strings.Count(got, "> Tip:"), "got %q", got)
			}
		})
	}
}
