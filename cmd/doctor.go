package cmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/doctor"
	"github.com/localstack/lstk/internal/emulator/aws"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newDoctorCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose your LocalStack environment",
		Long:  "Run read-only checks for configuration, Docker, authentication, and emulator connectivity.",
		RunE: commandWithTelemetry("doctor", tel, func(cmd *cobra.Command, args []string) error {
			configState, containers, err := loadDoctorConfig(cmd)
			if err != nil {
				return err
			}

			rt, rtErr := runtime.NewDockerRuntime(cfg.DockerHost)
			opts := doctor.Options{
				Config:           configState,
				Containers:       containers,
				LocalStackHost:   cfg.LocalStackHost,
				EnvAuthToken:     cfg.AuthToken,
				ForceFileKeyring: cfg.ForceFileKeyring,
				Logger:           logger,
				RuntimeInitError: rtErr,
			}

			awsClient := aws.NewClient(&http.Client{})
			if isInteractiveMode(cfg) {
				return ui.RunDoctor(cmd.Context(), rt, awsClient, opts)
			}
			return doctor.Run(cmd.Context(), rt, awsClient, output.NewPlainSink(os.Stdout), opts)
		}),
	}
}

func loadDoctorConfig(cmd *cobra.Command) (doctor.ConfigState, []config.ContainerConfig, error) {
	explicitPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return doctor.ConfigState{}, nil, err
	}

	if explicitPath != "" {
		return loadDoctorConfigPath(explicitPath)
	}

	existingPath, found, err := config.ExistingConfigFilePath()
	if err != nil {
		return doctor.ConfigState{}, nil, err
	}
	if found {
		return loadDoctorConfigPath(existingPath)
	}

	resolvedPath, err := config.ConfigFilePath()
	if err != nil {
		return doctor.ConfigState{}, nil, err
	}

	return doctor.ConfigState{
		Path:   resolvedPath,
		Exists: false,
	}, nil, nil
}

func loadDoctorConfigPath(path string) (doctor.ConfigState, []config.ContainerConfig, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return doctor.ConfigState{}, nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	state := doctor.ConfigState{
		Path:   absPath,
		Exists: true,
	}

	if _, err := os.Stat(absPath); err != nil {
		state.Exists = false
		state.LoadError = err
		return state, nil, nil
	}

	if err := config.InitFromPath(absPath); err != nil {
		state.LoadError = err
		return state, nil, nil
	}

	appConfig, err := config.Get()
	if err != nil {
		state.LoadError = err
		return state, nil, nil
	}

	state.Loaded = true
	return state, appConfig.Containers, nil
}
