package output

import (
	"fmt"
	"strings"
	"time"
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
	case InstanceInfoEvent:
		return formatInstanceInfo(e), true
	case ResourceSummaryEvent:
		return formatResourceSummary(e), true
	case ResourceTableEvent:
		return formatResourceTable(e)
	default:
		return "", false
	}
}

func formatStatusLine(e ContainerStatusEvent) (string, bool) {
	switch e.Phase {
	case "pulling":
		return "", false
	case "starting":
		return fmt.Sprintf("Starting %s...", e.Container), true
	case "waiting":
		return fmt.Sprintf("Waiting for %s to be ready...", e.Container), true
	case "ready":
		if e.Detail != "" {
			return fmt.Sprintf("%s ready (%s)", e.Container, e.Detail), true
		}
		return fmt.Sprintf("%s ready", e.Container), true
	default:
		if e.Detail != "" {
			return fmt.Sprintf("%s: %s (%s)", e.Container, e.Phase, e.Detail), true
		}
		return fmt.Sprintf("%s: %s", e.Container, e.Phase), true
	}
}


func formatUserInputRequest(e UserInputRequestEvent) string {
	return FormatPrompt(e.Prompt, e.Options)
}

// FormatPromptLabels returns the formatted label suffix for a prompt's options,
// e.g. " [Y/n]" for multiple options or " (Y)" for a single option.
func FormatPromptLabels(options []InputOption) string {
	labels := make([]string, 0, len(options))
	for _, opt := range options {
		if opt.Label != "" {
			labels = append(labels, opt.Label)
		}
	}
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf(" (%s)", labels[0])
	default:
		return fmt.Sprintf(" [%s]", strings.Join(labels, "/"))
	}
}

// FormatPrompt formats a prompt string with its options into a display line.
func FormatPrompt(prompt string, options []InputOption) string {
	lines := strings.Split(prompt, "\n")
	firstLine := lines[0] + FormatPromptLabels(options)
	rest := lines[1:]
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

func formatInstanceInfo(e InstanceInfoEvent) string {
	var sb strings.Builder
	sb.WriteString("✓ " + e.EmulatorName + " is running (" + e.Host + ")")
	var meta []string
	if e.Uptime > 0 {
		meta = append(meta, "UPTIME: "+formatUptime(e.Uptime))
	}
	if e.ContainerName != "" {
		meta = append(meta, "CONTAINER: "+e.ContainerName)
	}
	if e.Version != "" {
		meta = append(meta, "VERSION: "+e.Version)
	}
	if len(meta) > 0 {
		sb.WriteString("\n  " + strings.Join(meta, " · "))
	}
	return sb.String()
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatResourceSummary(e ResourceSummaryEvent) string {
	return fmt.Sprintf("~ %d resources · %d services", e.ResourceCount, e.ServiceCount)
}

func formatResourceTable(e ResourceTableEvent) (string, bool) {
	if len(e.Rows) == 0 {
		return "", false
	}
	return formatResourceTableWidth(e, terminalWidth()), true
}

func formatResourceTableWidth(e ResourceTableEvent, totalWidth int) string {
	headers := [4]string{"SERVICE", "RESOURCE", "REGION", "ACCOUNT"}

	// Compute natural (uncapped) column widths from data.
	widths := [4]int{len(headers[0]), len(headers[1]), len(headers[2]), len(headers[3])}
	for _, r := range e.Rows {
		cols := [4]string{r.Service, r.Resource, r.Region, r.Account}
		for i, c := range cols {
			if len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}

	// Fixed overhead: 2 (indent) + 3×2 (gaps between 4 columns) = 8.
	const overhead = 8
	// Cap SERVICE, REGION, ACCOUNT to their natural widths (they're short).
	// Give RESOURCE whatever space remains.
	fixedWidth := widths[0] + widths[2] + widths[3] + overhead
	maxResource := totalWidth - fixedWidth
	if maxResource < 10 {
		maxResource = 10
	}
	if widths[1] > maxResource {
		widths[1] = maxResource
	}

	maxWidths := [4]int{widths[0], widths[1], widths[2], widths[3]}

	var sb strings.Builder
	writeRow := func(cols [4]string) {
		sb.WriteString("  ")
		for i, c := range cols {
			val := truncate(c, maxWidths[i])
			sb.WriteString(val)
			if i < len(cols)-1 {
				padding := widths[i] - displayWidth(val) + 2
				for range padding {
					sb.WriteByte(' ')
				}
			}
		}
	}
	writeRow(headers)
	for _, r := range e.Rows {
		sb.WriteString("\n")
		writeRow([4]string{r.Service, r.Resource, r.Region, r.Account})
	}
	return sb.String()
}

