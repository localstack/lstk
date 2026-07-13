package cmd

import (
	"errors"
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

All resources created in the emulator (S3 buckets, Lambda functions, etc.) are discarded. The emulator keeps running; only its state is cleared.

To wipe the on-disk volume (certificates, persistence data, cached tools) instead, stop the emulator and run "lstk volume clear".`,
		PreRunE:     initConfig(nil),
		Annotations: map[string]string{jsonSupportedAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			sink := jsonAwareSink(cmd, cfg, os.Stdout)
			failWithCode := func(message string, code output.ErrorCode) error {
				bare := errors.New(message)
				if !cfg.JSON {
					return bare
				}
				sink.Emit(output.ErrorEvent{Title: message, Code: code})
				return output.NewSilentError(bare)
			}

			appConfig, err := config.Get()
			if err != nil {
				return failGetConfig(sink, cfg, err)
			}

			var awsContainer config.ContainerConfig
			var found bool
			for _, c := range appConfig.Containers {
				if c.Type == config.EmulatorAWS {
					awsContainer = c
					found = true
					break
				}
			}
			if !found {
				return failWithCode("reset is only supported for the AWS emulator", output.ErrEmulatorNotConfigured)
			}

			interactive := isInteractiveMode(cfg)
			if !interactive && !force {
				return failWithCode("reset requires confirmation; use --force to skip in non-interactive mode", output.ErrConfirmationRequired)
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
			resetter := aws.NewClient()

			if interactive {
				return ui.RunReset(cmd.Context(), rt, []config.ContainerConfig{awsContainer}, resetter, host, force)
			}
			return reset.Reset(cmd.Context(), rt, []config.ContainerConfig{awsContainer}, resetter, host, force, sink)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}
