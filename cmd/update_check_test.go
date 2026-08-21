package cmd

import (
	"bytes"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/update"
	"github.com/stretchr/testify/assert"
)

// TestResolveUpdateCheckMode covers the seam between what the user configured
// (LSTK_UPDATE_CHECK, [cli] update_check, the install-implied default) and the
// policy handed to update.NotifyUpdate.
func TestResolveUpdateCheckMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		checkCtx     updateCheckContext
		want         update.CheckMode
		wantWarnings []string
	}{
		{
			name:     "nothing set defaults to prompt",
			checkCtx: updateCheckContext{Interactive: true},
			want:     update.CheckModePrompt,
		},
		{
			name:     "nothing set on an externally managed install defaults to notify",
			checkCtx: updateCheckContext{ExternallyManaged: true, Interactive: true},
			want:     update.CheckModeNotify,
		},
		{
			name:     "config value is used when the env var is unset",
			checkCtx: updateCheckContext{ConfigValue: "off", Interactive: true},
			want:     update.CheckModeOff,
		},
		{
			name:     "env var beats config",
			checkCtx: updateCheckContext{EnvValue: "prompt", ConfigValue: "off", Interactive: true},
			want:     update.CheckModePrompt,
		},
		{
			name:     "explicit prompt overrides the externally managed default",
			checkCtx: updateCheckContext{ConfigValue: "prompt", ExternallyManaged: true, Interactive: true},
			want:     update.CheckModePrompt,
		},
		{
			name:     "explicit off overrides the externally managed default",
			checkCtx: updateCheckContext{EnvValue: "off", ExternallyManaged: true, Interactive: true},
			want:     update.CheckModeOff,
		},
		{
			// Only the TUI answers a prompt, so a non-interactive run notifies.
			name:     "prompt is downgraded to notify when not interactive",
			checkCtx: updateCheckContext{ConfigValue: "prompt"},
			want:     update.CheckModeNotify,
		},
		{
			name:     "off is honored when not interactive",
			checkCtx: updateCheckContext{ConfigValue: "off"},
			want:     update.CheckModeOff,
		},
		{
			name:         "invalid env value warns and falls through to config",
			checkCtx:     updateCheckContext{EnvValue: "yes", ConfigValue: "notify", Interactive: true},
			want:         update.CheckModeNotify,
			wantWarnings: []string{`> Warning: Ignoring LSTK_UPDATE_CHECK: invalid update_check value "yes" (must be one of: prompt, notify, off)`},
		},
		{
			name:     "invalid values in both sources warn and fall through to the default",
			checkCtx: updateCheckContext{EnvValue: "yes", ConfigValue: "disabled", ExternallyManaged: true, Interactive: true},
			want:     update.CheckModeNotify,
			wantWarnings: []string{
				`> Warning: Ignoring LSTK_UPDATE_CHECK: invalid update_check value "yes" (must be one of: prompt, notify, off)`,
				`> Warning: Ignoring update_check in [cli]: invalid update_check value "disabled" (must be one of: prompt, notify, off)`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			got := resolveUpdateCheckMode(output.NewPlainSink(&buf), tt.checkCtx)

			assert.Equal(t, tt.want, got)
			for _, warning := range tt.wantWarnings {
				assert.Contains(t, buf.String(), warning)
			}
			if len(tt.wantWarnings) == 0 {
				assert.Empty(t, buf.String(), "a valid configuration should print nothing")
			}
		})
	}
}
