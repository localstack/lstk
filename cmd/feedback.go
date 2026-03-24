package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/feedback"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/ui/styles"
	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newFeedbackCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Send feedback",
		Long:  "Send feedback directly to the LocalStack team.",
		RunE: commandWithTelemetry("feedback", tel, func(cmd *cobra.Command, args []string) error {
			sink := output.NewPlainSink(cmd.OutOrStdout())
			if !isInteractiveMode(cfg) {
				return fmt.Errorf("feedback requires an interactive terminal")
			}
			message, confirmed, err := collectFeedbackInteractively(cmd, sink, cfg)
			if err != nil {
				return err
			}
			if !confirmed {
				return nil
			}

			if strings.TrimSpace(cfg.AuthToken) == "" {
				return fmt.Errorf("feedback requires authentication")
			}
			client := feedback.NewClient(cfg.APIEndpoint)
			submit := func(ctx context.Context, submitSink output.Sink) error {
				output.EmitSpinnerStart(submitSink, "Submitting feedback")
				err := client.Submit(ctx, feedback.SubmitInput{
					Message:   message,
					AuthToken: cfg.AuthToken,
					Context:   buildFeedbackContext(cfg),
				})
				output.EmitSpinnerStop(submitSink)
				if err != nil {
					return err
				}
				output.EmitInfo(submitSink, styles.Success.Render(output.SuccessMarker())+" Thank you for your feedback!")
				return nil
			}

			err = ui.RunFeedback(cmd.Context(), submit)
			if err != nil {
				return err
			}
			return nil
		}),
	}
	return cmd
}

func collectFeedbackInteractively(cmd *cobra.Command, sink output.Sink, cfg *env.Env) (string, bool, error) {
	file, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return "", false, fmt.Errorf("interactive feedback requires a terminal")
	}

	output.EmitInfo(sink, "What's your feedback?")
	output.EmitSecondary(sink, styles.Secondary.Render("> Press enter to submit or esc to cancel"))

	message, cancelled, err := readInteractiveLine(file, cmd.OutOrStdout())
	if err != nil {
		return "", false, err
	}
	if cancelled {
		output.EmitSecondary(sink, styles.Secondary.Render("Cancelled feedback submission"))
		return "", false, nil
	}
	if strings.TrimSpace(message) == "" {
		return "", false, fmt.Errorf("feedback message cannot be empty")
	}

	ctx := buildFeedbackContext(cfg)
	output.EmitInfo(sink, "")
	output.EmitInfo(sink, "This report will include:")
	output.EmitInfo(sink, "- Feedback: "+styles.Secondary.Render(message))
	output.EmitInfo(sink, "- Version (lstk): "+styles.Secondary.Render(version.Version()))
	output.EmitInfo(sink, "- OS (arch): "+styles.Secondary.Render(fmt.Sprintf("%s (%s)", runtime.GOOS, runtime.GOARCH)))
	output.EmitInfo(sink, "- Installation: "+styles.Secondary.Render(orUnknown(ctx.InstallMethod)))
	output.EmitInfo(sink, "- Shell: "+styles.Secondary.Render(orUnknown(ctx.Shell)))
	output.EmitInfo(sink, "- Container runtime: "+styles.Secondary.Render(orUnknown(ctx.ContainerRuntime)))
	output.EmitInfo(sink, "- Auth: "+styles.Secondary.Render(authStatus(ctx.AuthConfigured)))
	output.EmitInfo(sink, "- Config: "+styles.Secondary.Render(orUnknown(ctx.ConfigPath)))
	output.EmitInfo(sink, "")
	output.EmitInfo(sink, renderConfirmationPrompt("Confirm submitting this feedback?"))

	submit, err := readConfirmation(file, cmd.OutOrStdout())
	if err != nil {
		return "", false, err
	}
	if !submit {
		output.EmitSecondary(sink, styles.Secondary.Render("Cancelled feedback submission"))
		return "", false, nil
	}
	return message, true, nil
}

func buildFeedbackContext(cfg *env.Env) feedback.Context {
	configPath, _ := config.ConfigFilePath()
	authConfigured := strings.TrimSpace(cfg.AuthToken) != ""
	if !authConfigured {
		if tokenStorage, err := auth.NewTokenStorage(cfg.ForceFileKeyring, log.Nop()); err == nil {
			if token, err := tokenStorage.GetAuthToken(); err == nil && strings.TrimSpace(token) != "" {
				authConfigured = true
			}
		}
	}
	return feedback.Context{
		AuthConfigured:   authConfigured,
		InstallMethod:    feedback.DetectInstallMethod(),
		Shell:            detectShell(),
		ContainerRuntime: detectContainerRuntime(cfg),
		ConfigPath:       configPath,
	}
}

func detectShell() string {
	shellPath := strings.TrimSpace(os.Getenv("SHELL"))
	if shellPath == "" {
		return "unknown"
	}
	return filepath.Base(shellPath)
}

func authStatus(v bool) string {
	if v {
		return "Configured"
	}
	return "Not Configured"
}

func detectContainerRuntime(cfg *env.Env) string {
	if strings.TrimSpace(cfg.DockerHost) != "" {
		return "docker"
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "docker"
	}

	switch {
	case fileExists(filepath.Join(homeDir, ".orbstack", "run", "docker.sock")):
		return "orbstack"
	case fileExists(filepath.Join(homeDir, ".colima", "default", "docker.sock")),
		fileExists(filepath.Join(homeDir, ".colima", "docker.sock")):
		return "colima"
	default:
		return "docker"
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func orUnknown(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return v
}

func renderConfirmationPrompt(question string) string {
	return styles.Secondary.Render("? ") +
		styles.Message.Render(question) +
		styles.Secondary.Render(" [Y/n]")
}

func readInteractiveLine(in *os.File, out io.Writer) (string, bool, error) {
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return "", false, err
	}
	defer func() { _ = term.Restore(int(in.Fd()), state) }()

	var buf []byte
	scratch := make([]byte, 1)
	for {
		if _, err := in.Read(scratch); err != nil {
			return "", false, err
		}
		switch scratch[0] {
		case '\r', '\n':
			_, _ = io.WriteString(out, "\r\n")
			return strings.TrimSpace(string(buf)), false, nil
		case 27:
			cancelled, err := readEscapeSequence(in)
			if err != nil {
				return "", false, err
			}
			if !cancelled {
				continue
			}
			_, _ = io.WriteString(out, "\r\n")
			return "", true, nil
		case 3:
			_, _ = io.WriteString(out, "\r\n")
			return "", true, nil
		case 127, 8:
			if len(buf) == 0 {
				continue
			}
			buf = buf[:len(buf)-1]
			_, _ = io.WriteString(out, "\b \b")
		default:
			if scratch[0] < 32 {
				continue
			}
			buf = append(buf, scratch[0])
			_, _ = out.Write(scratch)
		}
	}
}

func readConfirmation(in *os.File, out io.Writer) (bool, error) {
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return false, err
	}
	defer func() { _ = term.Restore(int(in.Fd()), state) }()

	scratch := make([]byte, 1)
	for {
		if _, err := in.Read(scratch); err != nil {
			return false, err
		}
		switch scratch[0] {
		case '\r', '\n', 'y', 'Y':
			_, _ = io.WriteString(out, "\r\n")
			return true, nil
		case 27:
			cancelled, err := readEscapeSequence(in)
			if err != nil {
				return false, err
			}
			if !cancelled {
				continue
			}
			_, _ = io.WriteString(out, "\r\n")
			return false, nil
		case 3, 'n', 'N':
			_, _ = io.WriteString(out, "\r\n")
			return false, nil
		}
	}
}
