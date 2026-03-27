package cmd

import (
	"github.com/localstack/lstk/internal/awscli"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/telemetry"
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
			return awscli.Exec(cmd.Context(), "http://"+host, args)
		}),
	}
}

func resolveAWSPort() string {
	const defaultPort = "4566"
	if err := config.Init(); err != nil {
		return defaultPort
	}
	appCfg, err := config.Get()
	if err != nil {
		return defaultPort
	}
	for _, c := range appCfg.Containers {
		if c.Type == config.EmulatorAWS {
			return c.Port
		}
	}
	return defaultPort
}
