package container

import (
	"bytes"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Detector cases live in the tips package (TestSelect_DetectorTipsYieldExactlyOneTip).
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
		{"unknown emulator", "other", false, true, 0},
		{"nothing started", "", true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 10 {
				var out bytes.Buffer
				emitPostStartTip(t.Context(), output.NewPlainSink(&out), tc.emulatorType, tc.firstRun, tc.interactive, nil, log.Nop())

				assert.Equal(t, tc.wantTipLineCount, strings.Count(out.String(), "> Tip:"))
			}
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
