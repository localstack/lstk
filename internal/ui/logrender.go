package ui

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
)

func renderLogLine(line string, level output.LogLevel) string {
	idx := strings.Index(line, " : ")
	if idx < 0 {
		return renderLogMessage(line, level)
	}
	meta := line[:idx+3] // up to and including " : "
	msg := line[idx+3:]
	return styles.Secondary.Render(meta) + renderLogMessage(msg, level)
}

func renderLogMessage(s string, level output.LogLevel) string {
	switch level {
	case output.LogLevelWarn:
		return styles.Warning.Render(s)
	case output.LogLevelError:
		return styles.LogError.Render(s)
	default:
		return s
	}
}
