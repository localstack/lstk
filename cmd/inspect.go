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
	cmd.AddCommand(newInspectSetCmd(cfg, true), newInspectSetCmd(cfg, false))
	return cmd
}

func newInspectSetCmd(cfg *env.Env, enabled bool) *cobra.Command {
	use, title := "enable", "Enable"
	if !enabled {
		use, title = "disable", "Disable"
	}

	return &cobra.Command{
		Use:         use,
		Short:       title + " App Inspector",
		Long:        title + " App Inspector on the running AWS emulator. The change lasts until the emulator restarts.",
		Args:        cobra.NoArgs,
		PreRunE:     initConfigDeferCreate(nil),
		Annotations: map[string]string{jsonSupportedAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			sink := jsonAwareSink(cmd, cfg, os.Stdout)
			t, err := resolveInspectTarget(cmd, cfg, sink)
			if err != nil {
				return err
			}
			if isInteractiveMode(cfg) {
				return ui.RunInspectSet(cmd.Context(), t, enabled)
			}
			return inspect.Set(cmd.Context(), sink, t, enabled)
		},
	}
}

func resolveInspectTarget(cmd *cobra.Command, cfg *env.Env, sink output.Sink) (inspect.Target, error) {
	fail := func(e output.ErrorEvent) (inspect.Target, error) {
		sink.Emit(e)
		return inspect.Target{}, output.NewSilentError(errors.New(e.Title))
	}

	remote, err := endpoint.Resolve(cmd.Context(), cmd)
	if err != nil {
		return fail(output.ErrorEvent{Title: err.Error(), Code: output.ErrEmulatorNotRunning})
	}
	if remote != nil {
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

	rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
	if err != nil {
		return inspect.Target{}, err
	}
	host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
	return inspect.Target{
		Runtime:    rt,
		Containers: []config.ContainerConfig{*awsContainer},
		Client:     aws.NewClient(),
		Host:       "http://" + host,
	}, nil
}
