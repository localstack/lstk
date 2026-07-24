package cmd

import (
	"context"
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
	"github.com/localstack/lstk/internal/terminal"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newAzCmd(cfg *env.Env) *cobra.Command {
	// DisableFlagParsing means Cobra won't strip lstk's own flags (a pre-command
	// --endpoint-url, --non-interactive, --config); PreRunE does that and
	// stashes the remaining args here for RunE to forward, mirroring
	// aws.go/terraform.go/cdk.go/sam.go's passthrough pattern (RunE's own args
	// parameter is NOT updated by PreRunE's local changes — Cobra calls each
	// independently with its own resolved args).
	var passthrough []string
	cmd := &cobra.Command{
		Use:   "az [args...]",
		Short: "Run Azure CLI commands against LocalStack",
		Long: `Run Azure CLI commands against the LocalStack Azure emulator.

'lstk az <args>' runs 'az <args>' with an isolated AZURE_CONFIG_DIR in which a custom Azure cloud is registered against LocalStack's endpoints, so your global ~/.azure configuration is left untouched and plain 'az' commands keep talking to real Azure. Run 'lstk setup azure' once before using this mode.

Alternatively, 'lstk az start-interception' redirects your global 'az' to LocalStack so existing scripts run unmodified, and 'lstk az stop-interception' switches back. Interception changes global state and is optional — prefer 'lstk az <args>' unless you specifically need plain 'az' to target LocalStack.

Examples:
  lstk az group list
  lstk az storage account list
  lstk az start-interception
  lstk az stop-interception`,
		DisableFlagParsing: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if jsonPrecedesCommandName(cmd.CalledAs()) {
				cfg.JSON = true
			}
			// --endpoint-url is recognized only when it precedes "az", the same
			// pre-command-only placement --json already gets here.
			if strippedArgs, v, ok := stripPreCommandEndpointURL(cmd.CalledAs()); ok {
				if err := cmd.Flags().Set(endpoint.FlagName, v); err != nil {
					return err
				}
				args = strippedArgs
			}

			var gf globalFlags
			passthrough, gf = stripGlobalFlags(args)
			if gf.nonInteractive {
				cfg.NonInteractive = true
			}
			if gf.configPath != "" {
				// initConfigDeferCreate reads the "config" flag, so feed the value back to it.
				if err := cmd.Flags().Set("config", gf.configPath); err != nil {
					return err
				}
			}
			return initConfigDeferCreate(nil)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			args := passthrough
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

			target, err := endpoint.Resolve(cmd.Context(), cmd)
			if err != nil {
				return emitValidationError(sink, err)
			}

			if _, err := azPreflight(cmd.Context(), cfg, sink, target); err != nil {
				return err
			}

			azEnv := azureconfig.Env(azureConfigDir)

			stdout, stderr := io.Writer(os.Stdout), io.Writer(os.Stderr)
			if !cfg.NonInteractive && terminal.IsTerminal(os.Stderr) {
				s := terminal.NewSpinner(os.Stderr, "Loading service...", 4*time.Second)
				s.Start()
				defer s.Stop()
				stdout = &terminal.StopOnWriteWriter{W: os.Stdout, Spinner: s}
				stderr = &terminal.StopOnWriteWriter{W: os.Stderr, Spinner: s}
			}

			// Only hand the Azure CLI a PTY when both of lstk's output streams
			// are the terminal: with stdout piped (`lstk az ... | grep`) the
			// child must keep seeing a pipe, or it would emit colors and CRLF
			// into the pipeline.
			usePTY := !cfg.NonInteractive && terminal.IsTerminal(os.Stdout) && terminal.IsTerminal(os.Stderr)

			return azurecli.Exec(cmd.Context(), azEnv, usePTY, os.Stdin, stdout, stderr, args...)
		},
	}

	cmd.AddCommand(newAzStartInterceptionCmd(cfg))
	cmd.AddCommand(newAzStopInterceptionCmd(cfg))
	return cmd
}

func newAzStartInterceptionCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "start-interception",
		Short:   "Redirect global 'az' to the LocalStack Azure emulator",
		Long:    "Register and activate a custom 'LocalStack' cloud in your global Azure CLI configuration (~/.azure) so that plain 'az' commands in any terminal target the LocalStack Azure emulator. This lets existing 'az' scripts run unmodified against LocalStack. It changes global state affecting every 'az' invocation until you run 'lstk az stop-interception'; this is independent of the isolated 'lstk az' setup.",
		Args:    cobra.NoArgs,
		PreRunE: initConfigDeferCreate(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			preflight := func(ctx context.Context, sink output.Sink) (string, error) {
				// --endpoint-url is out of scope for start-interception (see
				// design.md's Non-Goals) — always resolve via Docker.
				return azPreflight(ctx, cfg, sink, nil)
			}

			// Run preflight under the same sink as the operation so its errors render
			// in the TUI when interactive, instead of leaking plain output to stdout.
			if isInteractiveMode(cfg) {
				return ui.RunStartInterception(cmd.Context(), preflight)
			}

			sink := output.NewPlainSink(os.Stdout)
			endpointURL, err := preflight(cmd.Context(), sink)
			if err != nil {
				return err
			}
			return azureconfig.StartInterception(cmd.Context(), sink, endpointURL)
		},
	}
}

func newAzStopInterceptionCmd(cfg *env.Env) *cobra.Command {
	var cloud string
	c := &cobra.Command{
		Use:     "stop-interception",
		Short:   "Switch global 'az' back to real Azure",
		Long:    "Switch your global Azure CLI cloud away from the LocalStack emulator back to real Azure (AzureCloud by default; use --cloud to choose another registered cloud) and re-enable instance discovery. To avoid clobbering an unrelated selection, it only changes the active cloud when 'LocalStack' is currently active; otherwise it reports the current cloud and does nothing.",
		Args:    cobra.NoArgs,
		PreRunE: initConfigDeferCreate(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isInteractiveMode(cfg) {
				return ui.RunStopInterception(cmd.Context(), cloud)
			}
			return azureconfig.StopInterception(cmd.Context(), output.NewPlainSink(os.Stdout), cloud)
		},
	}
	c.Flags().StringVar(&cloud, "cloud", azureconfig.PublicCloudName, "Azure cloud to switch back to")
	return c
}

// azPreflight runs the checks shared by 'lstk az' passthrough and 'start-interception':
// the Azure CLI is installed, the Azure emulator is reachable (a managed container, or
// an already-running instance found via the HTTP probe when Docker discovery comes up
// empty), and *.localhost.localstack.cloud resolves. On failure it emits the matching
// ErrorEvent and returns a silent error. On success it returns the resolved LocalStack
// Azure endpoint URL.
//
// target, when non-nil, is an already-resolved externally-managed endpoint
// (--endpoint-url/LSTK_ENDPOINT_URL/AWS_ENDPOINT_URL): all of the Docker/DNS
// checks below are skipped in favor of it. 'start-interception'/
// 'stop-interception' always pass nil — they are out of scope for
// --endpoint-url (see design.md's Non-Goals).
func azPreflight(ctx context.Context, cfg *env.Env, sink output.Sink, target *endpoint.Target) (string, error) {
	if err := azurecli.CheckInstalled(); err != nil {
		sink.Emit(output.ErrorEvent{
			Title:   "az CLI not found in PATH",
			Actions: []output.ErrorAction{{Label: "Install Azure CLI:", Value: azurecli.InstallURL}},
		})
		return "", output.NewSilentError(err)
	}

	if target != nil {
		if target.Type != config.EmulatorAzure {
			err := fmt.Errorf("lstk az requires the Azure emulator, but the endpoint at %s is a %s emulator", target.URL, target.Type.DisplayName())
			sink.Emit(output.ErrorEvent{Title: err.Error()})
			return "", output.NewSilentError(err)
		}
		return azureconfig.BuildEndpoint(target.HostPort()), nil
	}

	appCfg, err := config.Get()
	if err != nil {
		return "", fmt.Errorf("failed to get config: %w", err)
	}
	azureContainer := config.ContainerConfig{Type: config.EmulatorAzure, Port: config.DefaultPort}
	for _, c := range appCfg.Containers {
		if c.Type == config.EmulatorAzure {
			azureContainer = c
			break
		}
	}

	resolvedHost, dnsOK := endpoint.ResolveHost(ctx, azureContainer.Port, cfg.LocalStackHost)

	resolved, _, err := resolveReachableEmulator(ctx, cfg.DockerHost, sink, azureContainer, resolvedHost)
	if err != nil {
		return "", err
	}
	if !resolved.Found() {
		return "", container.HandleNoRunningContainer(sink, azureContainer)
	}

	if !dnsOK {
		sink.Emit(output.ErrorEvent{
			Title: "DNS resolution required for 'lstk az'",
			Actions: []output.ErrorAction{
				{Label: "Note:", Value: "Could not resolve *." + endpoint.Hostname + " to 127.0.0.1."},
				{Label: "Why:", Value: "the Azure emulator serves endpoints under *." + endpoint.Hostname + ", which the Azure CLI must be able to resolve"},
				{Label: "Fix:", Value: "configure DNS or set LOCALSTACK_HOST"},
			},
		})
		return "", output.NewSilentError(fmt.Errorf("dns resolution required for 'lstk az'"))
	}

	return azureconfig.BuildEndpoint(resolvedHost), nil
}
