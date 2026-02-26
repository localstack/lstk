package output

import (
	"fmt"
	"strings"
)

// FormatEventLine converts an output event into a single display line.
func FormatEventLine(event any) (string, bool) {
	switch e := event.(type) {
	case LogEvent:
		return e.Message, true
	case HighlightLogEvent:
		return e.Message, true
	case SecondaryLogEvent:
		return e.Message, true
	case WarningEvent:
		return fmt.Sprintf("Warning: %s", e.Message), true
	case ContainerStatusEvent:
		return formatStatusLine(e), true
	case ProgressEvent:
		return formatProgressLine(e)
	case UserInputRequestEvent:
		return formatUserInputRequest(e), true
	case ContainerLogLineEvent:
		return e.Line, true
	default:
		return "", false
	}
}

func formatStatusLine(e ContainerStatusEvent) string {
	switch e.Phase {
	case "pulling":
		return fmt.Sprintf("Pulling %s...", e.Container)
	case "starting":
		return fmt.Sprintf("Starting %s...", e.Container)
	case "waiting":
		return fmt.Sprintf("Waiting for %s to be ready...", e.Container)
	case "ready":
		if e.Detail != "" {
			return fmt.Sprintf("%s ready (%s)", e.Container, e.Detail)
		}
		return fmt.Sprintf("%s ready", e.Container)
	default:
		if e.Detail != "" {
			return fmt.Sprintf("%s: %s (%s)", e.Container, e.Phase, e.Detail)
		}
		return fmt.Sprintf("%s: %s", e.Container, e.Phase)
	}
}

func formatProgressLine(e ProgressEvent) (string, bool) {
	if e.Total > 0 {
		pct := float64(e.Current) / float64(e.Total) * 100
		return fmt.Sprintf("  %s: %s %.1f%%", e.LayerID, e.Status, pct), true
	}
	if e.Status != "" {
		return fmt.Sprintf("  %s: %s", e.LayerID, e.Status), true
	}
	return "", false
}

func formatUserInputRequest(e UserInputRequestEvent) string {
	lines := strings.Split(e.Prompt, "\n")
	firstLine := lines[0]
	rest := lines[1:]
	labels := make([]string, 0, len(e.Options))
	for _, opt := range e.Options {
		if opt.Label != "" {
			labels = append(labels, opt.Label)
		}
	}

	switch len(labels) {
	case 0:
		if len(rest) == 0 {
			return firstLine
		}
		return strings.Join(append([]string{firstLine}, rest...), "\n")
	case 1:
		firstLine = fmt.Sprintf("%s (%s)", firstLine, labels[0])
	default:
		firstLine = fmt.Sprintf("%s [%s]", firstLine, strings.Join(labels, "/"))
	}

	if len(rest) == 0 {
		return firstLine
	}
	return strings.Join(append([]string{firstLine}, rest...), "\n")
}
