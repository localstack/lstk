package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start LocalStack",
	Long:  "Start the LocalStack emulator.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runStart(cmd.Context()); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func runStart(ctx context.Context) error {
	rt, err := runtime.NewDockerRuntime()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	// TODO: hardcoded for now, later should be configurable
	containers := []runtime.ContainerConfig{
		{
			Image: "localstack/localstack-pro:latest",
			Name:  "localstack-aws",
			Ports: map[string]string{"4566/tcp": "4566"},
		},
	}

	for _, config := range containers {
		fmt.Printf("Pulling %s...\n", config.Image)
		progress := make(chan runtime.PullProgress)
		go func() {
			for p := range progress {
				if p.Total > 0 {
					fmt.Printf("  %s: %s %.1f%%\n", p.LayerID, p.Status, float64(p.Current)/float64(p.Total)*100)
				} else if p.Status != "" {
					fmt.Printf("  %s: %s\n", p.LayerID, p.Status)
				}
			}
		}()
		if err := rt.PullImage(ctx, config.Image, progress); err != nil {
			return fmt.Errorf("failed to pull image %s: %w", config.Image, err)
		}

		fmt.Printf("Starting %s...\n", config.Name)
		containerID, err := rt.Start(ctx, config)
		if err != nil {
			return fmt.Errorf("failed to start %s: %w", config.Name, err)
		}

		running, err := rt.IsRunning(ctx, containerID)
		if err != nil {
			return fmt.Errorf("failed to check status of %s: %w", config.Name, err)
		}

		if !running {
			return fmt.Errorf("container %s failed to start", config.Name)
		}

		fmt.Printf("%s running (container: %s)\n", config.Name, containerID[:12])
	}

	return nil
}
