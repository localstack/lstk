package output

import (
	"testing"
)

// FuzzFormatPrompt targets lines[0] access after strings.Split — panics on empty input
// if Split returns empty slice (it won't for Go's Split, but exercises all paths).
func FuzzFormatPrompt(f *testing.F) {
	f.Add("", 0)
	f.Add("hello", 0)
	f.Add("line1\nline2", 0)
	f.Add("line1\nline2\nline3", 2)
	f.Add("\n", 0)
	f.Add("\n\n\n", 0)
	f.Add(string(make([]byte, 10000)), 0)
	f.Add(string([]byte{0}), 1)

	f.Fuzz(func(t *testing.T, prompt string, numOptions int) {
		if numOptions < 0 {
			numOptions = 0
		}
		if numOptions > 20 {
			numOptions = 20
		}

		options := make([]InputOption, numOptions)
		for i := range options {
			options[i] = InputOption{Label: prompt, Key: prompt}
		}

		// Must never panic
		_ = FormatPrompt(prompt, options)
		_ = FormatPromptLabels(options)
	})
}

// FuzzFormatEventLine exercises the type switch and all formatting paths.
func FuzzFormatEventLine(f *testing.F) {
	f.Add("", "", "", 0)
	f.Add("title", "summary", "detail", 3)
	f.Add(string(make([]byte, 5000)), "", "", 0)
	f.Add("\n\n\n", "\x00", "\xff", 10)

	f.Fuzz(func(t *testing.T, text, summary, detail string, actions int) {
		if actions < 0 {
			actions = 0
		}
		if actions > 50 {
			actions = 50
		}

		acts := make([]ErrorAction, actions)
		for i := range acts {
			acts[i] = ErrorAction{Label: text, Value: detail}
		}

		// Exercise all event types — none should panic
		FormatEventLine(MessageEvent{Text: text, Severity: SeveritySuccess})
		FormatEventLine(MessageEvent{Text: text, Severity: SeverityWarning})
		FormatEventLine(MessageEvent{Text: text, Severity: SeverityNote})
		FormatEventLine(MessageEvent{Text: text, Severity: SeveritySecondary})
		FormatEventLine(ErrorEvent{Title: text, Summary: summary, Detail: detail, Actions: acts})
		FormatEventLine(ContainerStatusEvent{Phase: text, Detail: detail})
		FormatEventLine(ContainerStatusEvent{Phase: "ready", Detail: detail})
		FormatEventLine(ContainerStatusEvent{Phase: "pulling"})
		FormatEventLine(AuthEvent{Preamble: text, URL: summary, Code: detail})
		FormatEventLine(LogLineEvent{Line: text})
		FormatEventLine(UserInputRequestEvent{Prompt: text})
		FormatEventLine(SpinnerEvent{Text: text, Active: true})
		FormatEventLine(SpinnerEvent{Text: text, Active: false})
	})
}

// FuzzFormatTableWidth exercises table formatting with adversarial dimensions.
func FuzzFormatTableWidth(f *testing.F) {
	f.Add(0, 0, 0, "")
	f.Add(1, 1, 80, "cell")
	f.Add(5, 10, 0, "data")
	f.Add(3, 3, -1, "x")
	f.Add(100, 1, 10, string(make([]byte, 500)))

	f.Fuzz(func(t *testing.T, ncols, nrows, width int, cellData string) {
		if ncols < 0 || ncols > 50 {
			return
		}
		if nrows < 0 || nrows > 50 {
			return
		}
		if width < 0 {
			width = 0
		}

		headers := make([]string, ncols)
		for i := range headers {
			headers[i] = cellData
		}

		rows := make([][]string, nrows)
		for i := range rows {
			row := make([]string, ncols)
			for j := range row {
				row[j] = cellData
			}
			rows[i] = row
		}

		// Must never panic
		e := TableEvent{Headers: headers, Rows: rows}
		formatTableWidth(e, width)
	})
}
