package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator/aws"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/inspect"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newInspectCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Turn App Inspector on or off",
		Long:  "Turn App Inspector on or off for the AWS emulator. App Inspector records the operations your application sends to the emulator, with their payloads, the services they connect, and IAM permission results. View them in the LocalStack Web Application or the LocalStack Toolkit for VS Code.",
	}
	requireSubcommand(cmd)
	cmd.AddCommand(newInspectSetCmd(cfg, true), newInspectSetCmd(cfg, false), newInspectStatusCmd(cfg))
	return cmd
}

func newInspectSetCmd(cfg *env.Env, enabled bool) *cobra.Command {
	var persist bool
	use, title, state := "enable", "Enable", "enabled"
	if !enabled {
		use, title, state = "disable", "Disable", "disabled"
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: title + " App Inspector",
		Long: title + " App Inspector on the running AWS emulator.\n\n" +
			"The change lasts until the emulator restarts. With --persist, lstk also saves it to the config file (as APP_INSPECTOR in an [env.app-inspector] profile), so App Inspector is " + state + " on every start. --persist works while the emulator is stopped too.",
		Args:        cobra.NoArgs,
		PreRunE:     initConfigDeferCreate(nil),
		Annotations: map[string]string{jsonSupportedAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			sink := jsonAwareSink(cmd, cfg, os.Stdout)
			t, err := resolveInspectTarget(cmd, cfg, sink, persist)
			if err != nil {
				return err
			}
			if isInteractiveMode(cfg) {
				return ui.RunInspectSet(cmd.Context(), t, enabled, persist)
			}
			return inspect.Set(cmd.Context(), sink, t, enabled, persist)
		},
	}
	cmd.Flags().BoolVar(&persist, "persist", false, "Also save the setting to the config file so it applies on every start")
	return cmd
}

func newInspectStatusCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:         "status",
		Short:       "Show whether App Inspector is enabled",
		Args:        cobra.NoArgs,
		PreRunE:     initConfigDeferCreate(nil),
		Annotations: map[string]string{jsonSupportedAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			sink := jsonAwareSink(cmd, cfg, os.Stdout)
			t, err := resolveInspectTarget(cmd, cfg, sink, false)
			if err != nil {
				return err
			}
			if isInteractiveMode(cfg) {
				return ui.RunInspectStatus(cmd.Context(), t)
			}
			return inspect.Status(cmd.Context(), sink, t)
		},
	}
}

func resolveInspectTarget(cmd *cobra.Command, cfg *env.Env, sink output.Sink, persist bool) (inspect.Target, error) {
	fail := func(e output.ErrorEvent) (inspect.Target, error) {
		sink.Emit(e)
		return inspect.Target{}, output.NewSilentError(errors.New(e.Title))
	}

	remote, err := endpoint.Resolve(cmd.Context(), cmd)
	if err != nil {
		return fail(output.ErrorEvent{Title: err.Error(), Code: output.ErrEmulatorNotRunning})
	}
	if remote != nil {
		if persist {
			source, _, _ := endpoint.ResolvedSource(cmd)
			return fail(output.ErrorEvent{
				Title:   "--persist cannot be used with " + source,
				Summary: "--persist saves to the lstk config, which does not start the emulator at " + remote.URL + ".",
				Code:    output.ErrValidationError,
			})
		}
		if remote.Type != config.EmulatorAWS {
			return fail(output.ErrorEvent{
				Title: fmt.Sprintf("App Inspector is only available for the AWS emulator, but the endpoint at %s is a %s", remote.URL, remote.Type.DisplayName()),
				Code:  output.ErrEmulatorWrongType,
			})
		}
		c := config.ContainerConfig{Type: config.EmulatorAWS, Port: config.DefaultPort}
		return inspect.Target{Runtime: runtime.NewExternalRuntime(c.Name()), Containers: []config.ContainerConfig{c}, Client: aws.NewClient(), Host: remote.URL}, nil
	}

	appConfig, err := config.Get()
	if err != nil {
		return inspect.Target{}, failGetConfig(sink, cfg, err)
	}
	var awsContainer *config.ContainerConfig
	for i := range appConfig.Containers {
		if appConfig.Containers[i].Type == config.EmulatorAWS {
			awsContainer = &appConfig.Containers[i]
			break
		}
	}
	if awsContainer == nil {
		return fail(output.ErrorEvent{Title: "App Inspector is only available for the AWS emulator", Code: output.ErrEmulatorNotConfigured})
	}
	if persist && !config.HasFile() {
		return fail(output.ErrorEvent{
			Title:   "No config file to save the setting to yet",
			Actions: []output.ErrorAction{{Label: "Create it by starting the emulator once:", Value: "lstk start"}},
			Code:    output.ErrConfigNotFound,
		})
	}

	rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
	if err != nil {
		return inspect.Target{}, err
	}
	host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
	path, err := config.FriendlyConfigPath()
	if err != nil {
		path = "the config file"
	}
	return inspect.Target{
		Runtime:    rt,
		Containers: []config.ContainerConfig{*awsContainer},
		Client:     aws.NewClient(),
		Host:       "http://" + host,
		ConfigPath: path,
	}, nil
}
