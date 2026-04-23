package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/localstack/lstk/internal/awscli"
	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/terminal"
	"github.com/spf13/cobra"
)

func newAWSCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
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
		PreRunE:            initConfig,
		RunE: commandWithTelemetry("aws", tel, func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			appCfg, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			awsContainer := config.ContainerConfig{Type: config.EmulatorAWS, Port: config.DefaultAWSPort}
			for _, c := range appCfg.Containers {
				if c.Type == config.EmulatorAWS {
					awsContainer = c
					break
				}
			}

			sink := output.NewPlainSink(os.Stdout)

			if err := rt.IsHealthy(cmd.Context()); err != nil {
				rt.EmitUnhealthyError(sink, err)
				return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
			}

			running, err := rt.IsRunning(cmd.Context(), awsContainer.Name())
			if err != nil {
				return fmt.Errorf("checking emulator status: %w", err)
			}
			if !running {
				sink.Emit(output.ErrorEvent{
					Title: fmt.Sprintf("%s is not running", awsContainer.DisplayName()),
					Actions: []output.ErrorAction{
						{Label: "Start LocalStack:", Value: "lstk"},
						{Label: "See help:", Value: "lstk -h"},
					},
				})
				return output.NewSilentError(fmt.Errorf("%s is not running", awsContainer.Name()))
			}

			host, _ := endpoint.ResolveHost(awsContainer.Port, cfg.LocalStackHost)

			profileExists, _ := awsconfig.ProfileExists()
			if !profileExists {
				sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "No AWS profile found, run 'lstk setup aws'"})
			}

			stdout, stderr := io.Writer(os.Stdout), io.Writer(os.Stderr)
			if terminal.IsTerminal(os.Stderr) {
				s := terminal.NewSpinner(os.Stderr, "Loading...")
				s.Start()
				defer s.Stop()
				stdout = &terminal.StopOnWriteWriter{W: os.Stdout, Spinner: s}
				stderr = &terminal.StopOnWriteWriter{W: os.Stderr, Spinner: s}
			}

			return awscli.Exec(cmd.Context(), "http://"+host, profileExists, stdout, stderr, args)
		}),
	}
}
