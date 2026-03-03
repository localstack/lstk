package output

import (
	"fmt"
	"strings"
)

// FormatEventLine converts an output event into a single display line.
func FormatEventLine(event any) (string, bool) {
	switch e := event.(type) {
	case MessageEvent:
		return formatMessageEvent(e), true
	case AuthEvent:
		return formatAuthEvent(e), true
	case SpinnerEvent:
		if e.Active {
			return e.Text + "...", true
		}
		return "", false
	case ErrorEvent:
		return formatErrorEvent(e), true
	case ContainerStatusEvent:
		return formatStatusLine(e)
	case ProgressEvent:
		return "", false
	case UserInputRequestEvent:
		return formatUserInputRequest(e), true
	case ContainerLogLineEvent:
		return e.Line, true
	default:
		return "", false
	}
}

func formatStatusLine(e ContainerStatusEvent) (string, bool) {
	switch e.Phase {
	case "pulling":
		return "Preparing LocalStack...", true
	case "starting":
		return "Starting LocalStack...", true
	case "waiting":
		return "Waiting for LocalStack to be ready...", true
	case "ready":
		if e.Detail != "" {
			return fmt.Sprintf("LocalStack ready (%s)", e.Detail), true
		}
		return "LocalStack ready", true
	default:
		if e.Detail != "" {
			return fmt.Sprintf("LocalStack: %s (%s)", e.Phase, e.Detail), true
		}
		return fmt.Sprintf("LocalStack: %s", e.Phase), true
	}
}


func formatUserInputRequest(e UserInputRequestEvent) string {
	return FormatPrompt(e.Prompt, e.Options)
}

// FormatPrompt formats a prompt string with its options into a display line.
func FormatPrompt(prompt string, options []InputOption) string {
	lines := strings.Split(prompt, "\n")
	firstLine := lines[0]
	rest := lines[1:]
	labels := make([]string, 0, len(options))
	for _, opt := range options {
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

func formatAuthEvent(e AuthEvent) string {
	var sb strings.Builder
	if e.Preamble != "" {
		sb.WriteString(e.Preamble)
		sb.WriteString("\n")
	}
	if e.Code != "" {
		sb.WriteString("Your one-time code: ")
		sb.WriteString(e.Code)
		sb.WriteString("\n")
	}
	if e.URL != "" {
		sb.WriteString("Opening browser to login...\n")
		sb.WriteString(e.URL)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatMessageEvent(e MessageEvent) string {
	switch e.Severity {
	case SeveritySuccess:
		return "> Success: " + e.Text
	case SeverityNote:
		return "> Note: " + e.Text
	case SeverityWarning:
		return "> Warning: " + e.Text
	default:
		return e.Text
	}
}

func formatErrorEvent(e ErrorEvent) string {
	var sb strings.Builder
	sb.WriteString("Error: ")
	sb.WriteString(e.Title)
	if e.Summary != "" {
		sb.WriteString("\n  ")
		sb.WriteString(e.Summary)
	}
	if e.Detail != "" {
		sb.WriteString("\n  ")
		sb.WriteString(e.Detail)
	}
	for _, action := range e.Actions {
		sb.WriteString("\n  → ")
		sb.WriteString(action.Label)
		sb.WriteString(" ")
		sb.WriteString(action.Value)
	}
	return sb.String()
}
