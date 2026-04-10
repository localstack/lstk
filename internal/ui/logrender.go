package ui

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
	"github.com/localstack/lstk/internal/ui/wrap"
)

func renderLogLine(line string, level output.LogLevel, availableWidth int, continuationIndent int) string {
	if availableWidth <= 0 {
		return renderStyledLogLine(line, level)
	}

	indent := strings.Repeat(" ", continuationIndent)
	logicalLines := strings.Split(line, "\n")
	rendered := make([]string, 0, len(logicalLines))
	firstOutputLine := true

	for _, logicalLine := range logicalLines {
		for _, part := range renderWrappedLogLine(logicalLine, level, availableWidth) {
			if !firstOutputLine {
				part = indent + part
			}
			rendered = append(rendered, part)
			firstOutputLine = false
		}
	}

	return strings.Join(rendered, "\n")
}

func renderStyledLogLine(line string, level output.LogLevel) string {
	idx := strings.Index(line, " : ")
	if idx < 0 {
		return renderLogMessage(line, level)
	}
	meta := line[:idx+3] // up to and including " : "
	msg := line[idx+3:]
	return styles.Secondary.Render(meta) + renderLogMessage(msg, level)
}

func renderWrappedLogLine(line string, level output.LogLevel, availableWidth int) []string {
	idx := strings.Index(line, " : ")
	if idx < 0 {
		return wrapStyledLogParts("", line, level, availableWidth)
	}

	meta := line[:idx+3] // up to and including " : "
	msg := line[idx+3:]
	return wrapStyledLogParts(meta, msg, level, availableWidth)
}

func wrapStyledLogParts(meta string, msg string, level output.LogLevel, availableWidth int) []string {
	plain := meta + msg
	if plain == "" {
		return []string{""}
	}

	metaRemaining := len([]rune(meta))
	parts := strings.Split(wrap.HardWrap(plain, availableWidth), "\n")
	wrapped := make([]string, 0, len(parts))

	for _, part := range parts {
		partRunes := []rune(part)
		metaCount := min(len(partRunes), metaRemaining)
		var sb strings.Builder

		if metaCount > 0 {
			sb.WriteString(styles.Secondary.Render(string(partRunes[:metaCount])))
			metaRemaining -= metaCount
		}
		if metaCount < len(partRunes) {
			sb.WriteString(renderLogMessage(string(partRunes[metaCount:]), level))
		}
		wrapped = append(wrapped, sb.String())
	}

	return wrapped
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
