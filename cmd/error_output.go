package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/localstack/lstk/internal/runtime"
)

var (
	errorTitleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#D04029"))
	errorBodyStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	errorNoteStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorSummaryPrefix    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorActionArrowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorActionTextStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	errorLinkStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
	errorK8sStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#A8C73A")).Bold(true)
)

func exitWithStartError(err error) {
	if runtimeErr, ok := runtime.AsRuntimeUnavailableError(err); ok {
		fmt.Fprintln(os.Stderr, errorTitleStyle.Render("✗ Could not start LocalStack"))
		fmt.Fprintln(os.Stderr, errorSummaryPrefix.Render("> ")+errorBodyStyle.Render("No container runtime detected."))
		printIndentedStyled(os.Stderr, summarizeRuntimeDetail(runtimeErr.Detail), "  ", errorNoteStyle)
		fmt.Fprintln(os.Stderr)
		printActionLine(os.Stderr, "Install Docker:", "https://docs.docker.com/get-started", errorBodyStyle)
		printActionLine(os.Stderr, "Using Podman? Run", "`lstk configure podman`", errorActionTextStyle)
		printActionLine(os.Stderr, "Deploying in Kubernetes?", "https://www.localstack.cloud/demo", errorK8sStyle)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func printIndentedStyled(out *os.File, text, indent string, style lipgloss.Style) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lineIndent := indent
		if i > 0 {
			lineIndent += "  "
		}
		fmt.Fprintln(out, lineIndent+style.Render(line))
	}
}

func printActionLine(out *os.File, label, value string, labelStyle lipgloss.Style) {
	fmt.Fprintln(
		out,
		errorActionArrowStyle.Render("⇒")+" "+
			labelStyle.Render(label)+" "+
			errorLinkStyle.Render(value),
	)
}

func summarizeRuntimeDetail(detail string) string {
	msg := strings.ToLower(detail)
	if strings.Contains(msg, "cannot connect to the docker daemon") &&
		strings.Contains(msg, "is the docker daemon running") {
		return "Cannot connect to Docker daemon."
	}
	return detail
}
