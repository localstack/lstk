package components

import (
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
