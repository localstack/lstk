package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator/aws"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/reset"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newResetCmd(cfg *env.Env) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset emulator state",
		Long: `Reset the running emulator's in-memory state.

All resources created in the emulator (S3 buckets, Lambda functions, etc.) are
discarded. The emulator keeps running; only its state is cleared.

To wipe the on-disk volume (certificates, persistence data, cached tools)
instead, stop the emulator and run "lstk volume clear".`,
		PreRunE: initConfig(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			var awsContainer *config.ContainerConfig
			for i, c := range appConfig.Containers {
				if c.Type == config.EmulatorAWS {
					awsContainer = &appConfig.Containers[i]
					break
				}
			}
			if awsContainer == nil {
				return errors.New("reset is only supported for the AWS emulator")
			}

			interactive := isInteractiveMode(cfg)
			if !interactive && !force {
				return errors.New("reset requires confirmation; use --force to skip in non-interactive mode")
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
			resetter := aws.NewClient()

			containers := []config.ContainerConfig{*awsContainer}

			if interactive {
				return ui.RunReset(cmd.Context(), rt, containers, resetter, host, force)
			}
			return reset.Reset(cmd.Context(), rt, containers, resetter, host, force, output.NewPlainSink(os.Stdout))
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}
