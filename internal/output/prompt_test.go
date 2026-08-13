package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmRendersInlineWithTheDefaultCapitalized(t *testing.T) {
	t.Parallel()

	// That the capitalized answer is the one ENTER selects is asserted end to end
	// by TestAppEnterHonorsTheConfirmDefault in internal/ui, which exercises the
	// real key resolution instead of a copy of its rule.
	tests := []struct {
		name   string
		def    ConfirmDefault
		labels []string
		hint   string
	}{
		{name: "default yes", def: DefaultYes, labels: []string{"Y", "n"}, hint: " [Y/n]"},
		{name: "default no", def: DefaultNo, labels: []string{"y", "N"}, hint: " [y/N]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ch := make(chan InputResponse, 1)
			event := Confirm("Reset emulator state?", tt.def, ch)

			assert.False(t, event.Vertical, "a confirmation stays on one line")
			require.Len(t, event.Options, 2)
			assert.Equal(t, KeyYes, event.Options[0].Key)
			assert.Equal(t, KeyNo, event.Options[1].Key)
			assert.Equal(t, tt.labels, []string{event.Options[0].Label, event.Options[1].Label})
			assert.Equal(t, "Reset emulator state?"+tt.hint, FormatPromptEvent(event))
		})
	}
}

func TestActionChoiceRendersVerticallyWithDerivedShortcuts(t *testing.T) {
	t.Parallel()

	ch := make(chan InputResponse, 1)
	event := ActionChoice("License validation failed: token expired.", []InputOption{
		{Key: "enter", Label: "Log in again"},
		{Key: "esc", Label: "Exit"},
	}, ch)

	assert.True(t, event.Vertical, "distinct actions render as selectable rows")
	assert.Equal(t, "[ENTER] Log in again", OptionLabel(event.Options[0]))
	assert.Equal(t, "[ESC] Exit", OptionLabel(event.Options[1]))

	// The one-line form keeps the shortcuts, so a prompt mirrored into spinner
	// text never leaves the user without a key to press. The labels bring their
	// own brackets, so it does not nest them inside an inline prompt's "[a/b]".
	assert.Equal(t,
		"License validation failed: token expired. [ENTER] Log in again / [ESC] Exit",
		FormatPromptEvent(event))
}

func TestAcknowledgeRendersInlineWithASingleAnyKeyOption(t *testing.T) {
	t.Parallel()

	ch := make(chan InputResponse, 1)
	event := Acknowledge("Waiting for authorization...", "Press any key when complete", ch)

	assert.False(t, event.Vertical)
	require.Len(t, event.Options, 1)
	assert.Equal(t, KeyAny, event.Options[0].Key)
	assert.Equal(t, "Waiting for authorization... (Press any key when complete)", FormatPromptEvent(event))
}

func TestOptionLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opt  InputOption
		want string
	}{
		{name: "named key", opt: InputOption{Key: "enter", Label: "Log in again"}, want: "[ENTER] Log in again"},
		{name: "escape", opt: InputOption{Key: "esc", Label: "Exit"}, want: "[ESC] Exit"},
		{name: "single letter is uppercased", opt: InputOption{Key: "a", Label: "AWS"}, want: "[A] AWS"},
		{name: "any key advertises nothing", opt: InputOption{Key: KeyAny, Label: "Press any key"}, want: "Press any key"},
		{name: "empty key advertises nothing", opt: InputOption{Label: "Continue"}, want: "Continue"},
		{name: "no label", opt: InputOption{Key: "w"}, want: "[W]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, OptionLabel(tt.opt))
		})
	}
}
