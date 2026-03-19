package container

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
)

func shouldFilter(line string) bool {
	if strings.Contains(line, "Docker not available") {
		return true
	}
	_, logger := parseLogLine(line)
	switch {
	case logger == "localstack.request.http":
		return true
	case logger == "l.aws.handlers.internal":
		return true
	case strings.HasSuffix(logger, ".provider"):
		return true
	}
	return false
}

// Expected format: 2026-03-16T17:56:00.810  INFO --- [  MainThread] l.p.c.extensions.plugins   : message
func parseLogLine(line string) (output.LogLevel, string) {
	sepIdx := strings.Index(line, " --- [")
	if sepIdx < 0 {
		return output.LogLevelUnknown, ""
	}

	prefix := strings.TrimSpace(line[:sepIdx])
	level := output.LogLevelUnknown
	if spIdx := strings.LastIndex(prefix, " "); spIdx >= 0 {
		switch prefix[spIdx+1:] {
		case "DEBUG":
			level = output.LogLevelDebug
		case "INFO":
			level = output.LogLevelInfo
		case "WARN":
			level = output.LogLevelWarn
		case "ERROR":
			level = output.LogLevelError
		}
	}

	rest := line[sepIdx+6:] // skip " --- ["
	bracketEnd := strings.Index(rest, "]")
	if bracketEnd < 0 {
		return level, ""
	}
	afterBracket := strings.TrimSpace(rest[bracketEnd+1:])
	colonIdx := strings.Index(afterBracket, " : ")
	if colonIdx < 0 {
		return level, ""
	}
	logger := strings.TrimSpace(afterBracket[:colonIdx])
	return level, logger
}
