package container

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
)

// shouldFilter returns true if the log line is identified as noise and should not be shown.
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

// parseLogLine extracts the log level and logger name from a LocalStack log line.
// Expected format: 2026-03-16T17:56:00.810  INFO --- [  MainThread] l.p.c.extensions.plugins   : message
// Returns LogLevelUnknown and empty string if the line does not match the expected format.
func parseLogLine(line string) (output.LogLevel, string) {
	// Find the thread section separator
	sepIdx := strings.Index(line, " --- [")
	if sepIdx < 0 {
		return output.LogLevelUnknown, ""
	}

	// Level is the last word before " --- ["
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

	// Logger is between "] " and "   : "
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
