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
	case WarningEvent:
		return fmt.Sprintf("Warning: %s", e.Message), true
	case ContainerStatusEvent:
		return formatStatusLine(e), true
	case ProgressEvent:
		return formatProgressLine(e)
	case UserInputRequestEvent:
		return formatUserInputRequest(e), true
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
	switch len(e.Options) {
	case 0:
		return e.Prompt
	case 1:
		return fmt.Sprintf("%s (%s)", e.Prompt, e.Options[0].Label)
	default:
		labels := make([]string, len(e.Options))
		for i, opt := range e.Options {
			labels[i] = opt.Label
		}
		return fmt.Sprintf("%s [%s]", e.Prompt, strings.Join(labels, "/"))
	}
}
