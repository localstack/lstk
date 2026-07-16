package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/localstack/lstk/internal/awscli"
	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/terminal"
	"github.com/spf13/cobra"
)

func newAWSCmd(cfg *env.Env) *cobra.Command {
	// DisableFlagParsing means Cobra won't strip lstk's own flags; PreRunE does
	// that and stashes the remaining args here for RunE to forward to aws.
	var passthrough []string
	return &cobra.Command{
		Use:   "aws [args...]",
		Short: "Run AWS CLI commands against LocalStack",
		Long: `Proxy AWS CLI commands to LocalStack with endpoint, credentials, and region pre-configured.

Equivalent to running:
  aws --endpoint-url http://localhost:4566 <args>
with AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, and AWS_DEFAULT_REGION set automatically.

Run 'lstk setup aws' to configure the LocalStack AWS profile for use with CLI and SDKs.

Examples:
  lstk aws s3 ls
  lstk aws sqs list-queues
  lstk aws s3 mb s3://my-bucket`,
		DisableFlagParsing: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			var gf globalFlags
			passthrough, gf = stripGlobalFlags(args)
			if gf.nonInteractive {
				cfg.NonInteractive = true
			}
			if jsonPrecedesCommandName(cmd.CalledAs()) {
				cfg.JSON = true
			}
			if gf.configPath != "" {
				// initConfig reads the "config" flag, so feed the value back to it.
				if err := cmd.Flags().Set("config", gf.configPath); err != nil {
					return err
				}
			}
			return initConfig(nil)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			sink := output.NewPlainSink(os.Stdout)

			if err := awscli.CheckInstalled(); err != nil {
				sink.Emit(output.ErrorEvent{
					Title:   "aws CLI not found in PATH",
					Actions: []output.ErrorAction{{Label: "Install AWS CLI:", Value: awscli.InstallURL}},
				})
				return output.NewSilentError(err)
			}

			// --help/-h never contacts LocalStack, so it runs directly without
			// requiring Docker or a running emulator (DEVX-1002).
			if awscli.IsHelp(passthrough) {
				return awscli.Exec(cmd.Context(), "", false, os.Stdout, os.Stderr, passthrough)
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			appCfg, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			awsContainer := config.ContainerConfig{Type: config.EmulatorAWS, Port: config.DefaultPort}
			for _, c := range appCfg.Containers {
				if c.Type == config.EmulatorAWS {
					awsContainer = c
					break
				}
			}

			if err := rt.IsHealthy(cmd.Context()); err != nil {
				rt.EmitUnhealthyError(sink, err)
				return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
			}

			runningName, err := container.ResolveRunningContainerName(cmd.Context(), rt, awsContainer)
			if err != nil {
				return fmt.Errorf("checking emulator status: %w", err)
			}
			if runningName == "" {
				sink.Emit(output.ErrorEvent{
					Title: fmt.Sprintf("%s is not running", awsContainer.DisplayName()),
					Actions: []output.ErrorAction{
						{Label: "Start LocalStack:", Value: "lstk"},
						{Label: "See help:", Value: "lstk -h"},
					},
				})
				return output.NewSilentError(fmt.Errorf("%s is not running", awsContainer.Name()))
			}

			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)

			profileExists, _ := awsconfig.ProfileExists(cmd.Context())
			if !profileExists {
				sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "No AWS profile found, run 'lstk setup aws'"})
			}

			stdout, stderr := io.Writer(os.Stdout), io.Writer(os.Stderr)
			if !cfg.NonInteractive && terminal.IsTerminal(os.Stderr) {
				s := terminal.NewSpinner(os.Stderr, "Loading service...", 4*time.Second)
				s.Start()
				defer s.Stop()
				stdout = &terminal.StopOnWriteWriter{W: os.Stdout, Spinner: s}
				stderr = &terminal.StopOnWriteWriter{W: os.Stderr, Spinner: s}
			}

			return awscli.Exec(cmd.Context(), "http://"+host, profileExists, stdout, stderr, passthrough)
		},
	}
}
