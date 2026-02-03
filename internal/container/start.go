package container

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/runtime"
)

func Start(ctx context.Context, rt runtime.Runtime, onProgress func(string)) error {
	token, err := auth.GetToken(ctx)
	if err != nil {
		return err
	}

	cfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	containers := make([]runtime.ContainerConfig, len(cfg.Containers))
	for i, c := range cfg.Containers {
		image, err := c.Image()
		if err != nil {
			return err
		}

		env := append(c.Env, "LOCALSTACK_AUTH_TOKEN="+token)
		containers[i] = runtime.ContainerConfig{
			Image:      image,
			Name:       c.Name(),
			Port:       c.Port,
			HealthPath: c.HealthPath,
			Env:        env,
		}
	}

	for _, config := range containers {
		onProgress(fmt.Sprintf("Pulling %s...", config.Image))
		progress := make(chan runtime.PullProgress)
		go func() {
			for p := range progress {
				if p.Total > 0 {
					onProgress(fmt.Sprintf("  %s: %s %.1f%%", p.LayerID, p.Status, float64(p.Current)/float64(p.Total)*100))
				} else if p.Status != "" {
					onProgress(fmt.Sprintf("  %s: %s", p.LayerID, p.Status))
				}
			}
		}()
		if err := rt.PullImage(ctx, config.Image, progress); err != nil {
			return fmt.Errorf("failed to pull image %s: %w", config.Image, err)
		}

		onProgress(fmt.Sprintf("Starting %s...", config.Name))
		containerID, err := rt.Start(ctx, config)
		if err != nil {
			return fmt.Errorf("failed to start %s: %w", config.Name, err)
		}

		onProgress(fmt.Sprintf("Waiting for %s to be ready...", config.Name))
		healthURL := fmt.Sprintf("http://localhost:%s%s", config.Port, config.HealthPath)
		if err := awaitStartup(ctx, rt, containerID, config.Name, healthURL); err != nil {
			return err
		}

		onProgress(fmt.Sprintf("%s ready (container: %s)", config.Name, containerID[:12]))
	}

	return nil
}

// awaitStartup polls until one of two outcomes:
//   - Success: health endpoint returns 200 (license is valid, LocalStack is ready)
//   - Failure: container stops running (e.g., license activation failed), returns error with container logs
//
// TODO: move to Runtime interface if other runtimes (k8s?) need native readiness probes
func awaitStartup(ctx context.Context, rt runtime.Runtime, containerID, name, healthURL string) error {
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		running, err := rt.IsRunning(ctx, containerID)
		if err != nil {
			return fmt.Errorf("failed to check container status: %w", err)
		}
		if !running {
			logs, logsErr := rt.Logs(ctx, containerID, 20)
			if logsErr != nil || logs == "" {
				return fmt.Errorf("%s exited unexpectedly", name)
			}
			return fmt.Errorf("%s exited unexpectedly:\n%s", name, logs)
		}

		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}
