package cmd

import (
	"io"
	"os"

	"github.com/localstack/lstk/internal/awscli"
	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
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

Examples:
  lstk aws s3 ls
  lstk aws sqs list-queues
  lstk aws s3 mb s3://my-bucket`,
		DisableFlagParsing: true,
		RunE: commandWithTelemetry("aws", tel, func(cmd *cobra.Command, args []string) error {
			port := resolveAWSPort()
			host, _ := endpoint.ResolveHost(port, cfg.LocalStackHost)

			profileExists, _ := awsconfig.ProfileExists()
			if !profileExists {
				output.EmitNote(output.NewPlainSink(os.Stdout), "No AWS profile found, run 'lstk setup aws'")
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

func resolveAWSPort() string {
	if err := config.Init(); err != nil {
		return config.DefaultAWSPort
	}
	appCfg, err := config.Get()
	if err != nil {
		return config.DefaultAWSPort
	}
	for _, c := range appCfg.Containers {
		if c.Type == config.EmulatorAWS {
			return c.Port
		}
	}
	return config.DefaultAWSPort
}
