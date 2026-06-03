package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/localstack/lstk/internal/azurecli"
	"github.com/localstack/lstk/internal/azureconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/terminal"
	"github.com/spf13/cobra"
)

func newAzCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "az [args...]",
		Short: "Run Azure CLI commands against LocalStack",
		Long: `Run Azure CLI commands against the LocalStack Azure emulator.

Runs 'az <args>' with an isolated AZURE_CONFIG_DIR in which a custom Azure cloud is registered against LocalStack's endpoints, so your global ~/.azure configuration is left untouched and plain 'az' commands keep talking to real Azure.

Run 'lstk setup azure' once before using this command.

Examples:
  lstk az group list
  lstk az storage account list`,
		DisableFlagParsing: true,
		PreRunE:            initConfig(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			appCfg, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			azureContainer := config.ContainerConfig{Type: config.EmulatorAzure, Port: config.DefaultPort}
			for _, c := range appCfg.Containers {
				if c.Type == config.EmulatorAzure {
					azureContainer = c
					break
				}
			}

			sink := output.NewPlainSink(os.Stdout)

			configDir, err := config.ConfigDir()
			if err != nil {
				return fmt.Errorf("failed to resolve config directory: %w", err)
			}
			azureConfigDir := azureconfig.ConfigDir(configDir)
			if !azureconfig.IsSetUp(azureConfigDir) {
				sink.Emit(output.ErrorEvent{
					Title: "Azure CLI integration is not set up",
					Actions: []output.ErrorAction{
						{Label: "Set it up:", Value: "lstk setup azure"},
					},
				})
				return output.NewSilentError(fmt.Errorf("azure CLI integration not set up"))
			}

			if err := rt.IsHealthy(cmd.Context()); err != nil {
				rt.EmitUnhealthyError(sink, err)
				return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
			}

			runningName, err := container.ResolveRunningContainerName(cmd.Context(), rt, azureContainer)
			if err != nil {
				return fmt.Errorf("checking emulator status: %w", err)
			}
			if runningName == "" {
				sink.Emit(output.ErrorEvent{
					Title: fmt.Sprintf("%s is not running", azureContainer.DisplayName()),
					Actions: []output.ErrorAction{
						{Label: "Start LocalStack:", Value: "lstk"},
						{Label: "See help:", Value: "lstk -h"},
					},
				})
				return output.NewSilentError(fmt.Errorf("%s is not running", azureContainer.Name()))
			}

			_, dnsOK := endpoint.ResolveHost(cmd.Context(), azureContainer.Port, cfg.LocalStackHost)
			if !dnsOK {
				sink.Emit(output.ErrorEvent{
					Title: "DNS resolution required for 'lstk az'",
					Actions: []output.ErrorAction{
						{Label: "Note:", Value: "Could not resolve *." + endpoint.Hostname + " to 127.0.0.1."},
						{Label: "Why:", Value: "the Azure emulator serves endpoints under *." + endpoint.Hostname + ", which the Azure CLI must be able to resolve"},
						{Label: "Fix:", Value: "configure DNS or set LOCALSTACK_HOST"},
					},
				})
				return output.NewSilentError(fmt.Errorf("dns resolution required for 'lstk az'"))
			}

			azEnv := azureconfig.Env(azureConfigDir)

			stdout, stderr := io.Writer(os.Stdout), io.Writer(os.Stderr)
			if terminal.IsTerminal(os.Stderr) {
				s := terminal.NewSpinner(os.Stderr, "Loading service...", 4*time.Second)
				s.Start()
				defer s.Stop()
				stdout = &terminal.StopOnWriteWriter{W: os.Stdout, Spinner: s}
				stderr = &terminal.StopOnWriteWriter{W: os.Stderr, Spinner: s}
			}

			return azurecli.Exec(cmd.Context(), azEnv, os.Stdin, stdout, stderr, args...)
		},
	}
}
