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
	case LogLineEvent:
		return e.Line, true
	case InstanceInfoEvent:
		return formatInstanceInfo(e), true
	case TableEvent:
		return formatTable(e)
	case ResourceSummaryEvent:
		return formatResourceSummary(e), true
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

// FormatPromptLabels formats option labels into a suffix string.
// Returns " (label)" for a single option, " [a/b]" for multiple, or "" for none.
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
	if e.URL != "" {
		sb.WriteString("Opening browser to login...")
		sb.WriteString("\n")
		sb.WriteString("Browser didn't open? Visit ")
		sb.WriteString(e.URL)
	}
	if e.Code != "" {
		sb.WriteString("\n\nOne-time code: ")
		sb.WriteString(e.Code)
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
		sb.WriteString("\n  " + ErrorActionPrefix)
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
	return fmt.Sprintf("~ %d resources · %d services", e.Resources, e.Services)
}

func formatTable(e TableEvent) (string, bool) {
	if len(e.Rows) == 0 {
		return "", false
	}
	return formatTableWidth(e, terminalWidth()), true
}

func formatTableWidth(e TableEvent, totalWidth int) string {
	ncols := len(e.Headers)
	if ncols == 0 {
		return ""
	}

	widths := make([]int, ncols)
	for i, h := range e.Headers {
		widths[i] = len(h)
	}
	for _, row := range e.Rows {
		for i := range min(len(row), ncols) {
			if len(row[i]) > widths[i] {
				widths[i] = len(row[i])
			}
		}
	}

	// When totalWidth is 0 (stdout is not a TTY), skip truncation entirely.
	if totalWidth > 0 {
		// Fixed overhead: 2 (indent) + (ncols-1)*2 (gaps between columns).
		overhead := 2 + (ncols-1)*2

		// Find the widest column and let it absorb any overflow.
		maxCol := 0
		for i := 1; i < ncols; i++ {
			if widths[i] > widths[maxCol] {
				maxCol = i
			}
		}
		fixedWidth := overhead
		for i, w := range widths {
			if i != maxCol {
				fixedWidth += w
			}
		}
		maxFlexible := totalWidth - fixedWidth
		if maxFlexible < 10 {
			maxFlexible = 10
		}
		if widths[maxCol] > maxFlexible {
			widths[maxCol] = maxFlexible
		}
	}

	var sb strings.Builder
	writeRow := func(cols []string) {
		sb.WriteString("  ")
		for i := range ncols {
			cell := ""
			if i < len(cols) {
				cell = cols[i]
			}
			val := truncate(cell, widths[i])
			sb.WriteString(val)
			if i < ncols-1 {
				padding := widths[i] - displayWidth(val) + 2
				for range padding {
					sb.WriteByte(' ')
				}
			}
		}
	}
	upperHeaders := make([]string, len(e.Headers))
	for i, h := range e.Headers {
		upperHeaders[i] = strings.ToUpper(h)
	}
	writeRow(upperHeaders)
	for _, row := range e.Rows {
		sb.WriteString("\n")
		writeRow(row)
	}
	return sb.String()
}
