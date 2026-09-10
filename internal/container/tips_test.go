package container

import (
	"bytes"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tip names the command instead of linking to docs (#495 review): a URL in
// terminal output cannot be clicked, rots, and drifts from the CLI, while
// `lstk completion` documents itself.
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
	assert.Contains(t, tipsForType(config.EmulatorAWS), got)
}

func TestSelectTip_SubsequentRun_ReturnsRotatingTip(t *testing.T) {
	for range 20 {
		got := selectTip(config.EmulatorAWS, false, true)

		assert.NotEqual(t, completionTip, got, "the completion tip must not repeat past the first run")
		assert.Contains(t, tipsForType(config.EmulatorAWS), got)
	}
}

func TestSelectTip_UnknownEmulator_ReturnsNoTip(t *testing.T) {
	assert.Empty(t, selectTip(config.EmulatorType("other"), false, true))
}

func TestSelectTip_UnknownEmulatorOnFirstRun_StillReturnsCompletionTip(t *testing.T) {
	assert.Equal(t, completionTip, selectTip(config.EmulatorType("other"), true, true),
		"the completion tip is about lstk itself, not the emulator that happens to be configured")
}

// No input combination can produce two lines.
func TestEmitPostStartTip_EmitsAtMostOneTipLine(t *testing.T) {
	for _, tc := range []struct {
		name             string
		emulatorType     config.EmulatorType
		firstRun         bool
		interactive      bool
		wantTipLineCount int
	}{
		{"first run interactive", config.EmulatorAWS, true, true, 1},
		{"first run non-interactive", config.EmulatorAWS, true, false, 1},
		{"subsequent run", config.EmulatorAWS, false, true, 1},
		{"unknown emulator", config.EmulatorType("other"), false, true, 0},
		{"nothing started", config.EmulatorType(""), true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			sink := output.NewPlainSink(&out)

			emitPostStartTip(sink, tc.emulatorType, tc.firstRun, tc.interactive)

			assert.Equal(t, tc.wantTipLineCount, strings.Count(out.String(), "> Tip:"))
		})
	}
}

// The pointers block must not carry a tip of its own: Start is the single emit site.
func TestEmitPostStartPointers_EmitsNoTip(t *testing.T) {
	for _, emulatorType := range []config.EmulatorType{config.EmulatorAWS, config.EmulatorSnowflake, config.EmulatorAzure} {
		var out bytes.Buffer
		sink := output.NewPlainSink(&out)

		emitPostStartPointers(sink, emulatorType, "localhost.localstack.cloud:4566", "https://app.localstack.cloud/", true)

		require.NotContains(t, out.String(), "> Tip:", "%s pointers block emitted a tip", emulatorType)
	}
}
