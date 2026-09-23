package components

import (
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
	"github.com/stretchr/testify/assert"
)

func TestRenderMessage_SecondaryUsesSubduedStyle(t *testing.T) {
	tests := []string{
		"• Endpoint: localhost.localstack.cloud:4566",
		"• Web app: https://app.localstack.cloud",
		"> Tip: View emulator logs: lstk logs --follow",
	}

	for _, text := range tests {
		assert.Equal(t, styles.SecondaryMessage.Render(text), RenderMessage(output.MessageEvent{
			Severity: output.SeveritySecondary,
			Text:     text,
		}))
	}
}

func TestRenderMessage_LeavesRegularInfoLinesUnchanged(t *testing.T) {
	assert.Equal(t, styles.Message.Render("hello"), RenderMessage(output.MessageEvent{
		Severity: output.SeverityInfo,
		Text:     "hello",
	}))
}

// An explicit line break survives wrapping, and every continuation line hangs
// under the text rather than under the prefix.
func TestRenderWrappedMessage_KeepsExplicitLineBreaksAligned(t *testing.T) {
	e := output.MessageEvent{
		Severity: output.SeverityWarning,
		Text:     "a sentence long enough to wrap onto a second line before the link:\nhttps://example.com/latest",
	}
	for _, width := range []int{0, 40, 200} {
		lines := strings.Split(RenderWrappedMessage(e, width), "\n")
		last := lines[len(lines)-1]
		assert.Equal(t, strings.Repeat(" ", len("> Warning: "))+"https://example.com/latest", last, "width %d", width)
		for _, line := range lines[1:] {
			assert.True(t, strings.HasPrefix(line, strings.Repeat(" ", len("> Warning: "))), "width %d: %q", width, line)
		}
	}
}
