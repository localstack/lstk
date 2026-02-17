package container

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	stdruntime "runtime"
	"time"

	"github.com/containerd/errdefs"
	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func Start(ctx context.Context, rt runtime.Runtime, sink output.Sink, platformClient api.PlatformAPI, interactive bool) error {
	tokenStorage, err := auth.NewTokenStorage()
	if err != nil {
		return fmt.Errorf("failed to initialize token storage: %w", err)
	}
	a := auth.New(sink, platformClient, tokenStorage, interactive)

	token, err := a.GetToken(ctx)
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
		healthPath, err := c.HealthPath()
		if err != nil {
			return err
		}

		env := append(c.Env, "LOCALSTACK_AUTH_TOKEN="+token)
		containers[i] = runtime.ContainerConfig{
			Image:      image,
			Name:       c.Name(),
			Port:       c.Port,
			HealthPath: healthPath,
			Env:        env,
		}
	}

	containers, cfg.Containers, err = selectContainersToStart(ctx, rt, sink, containers, cfg.Containers)
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return nil
	}

	// TODO validate license for tag "latest" without resolving the actual image version,
	// and avoid pulling all images first
	if err := pullImages(ctx, rt, sink, containers); err != nil {
		return err
	}

	if err := validateLicenses(ctx, rt, sink, platformClient, containers, cfg.Containers, token); err != nil {
		return err
	}

	return startContainers(ctx, rt, sink, containers)
}

func pullImages(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []runtime.ContainerConfig) error {
	for _, c := range containers {
		// Remove any existing stopped container with the same name
		if err := rt.Remove(ctx, c.Name); err != nil && !errdefs.IsNotFound(err) {
			return fmt.Errorf("failed to remove existing container %s: %w", c.Name, err)
		}

		output.EmitStatus(sink, "pulling", c.Image, "")
		progress := make(chan runtime.PullProgress)
		go func() {
			for p := range progress {
				output.EmitProgress(sink, c.Image, p.LayerID, p.Status, p.Current, p.Total)
			}
		}()
		if err := rt.PullImage(ctx, c.Image, progress); err != nil {
			return fmt.Errorf("failed to pull image %s: %w", c.Image, err)
		}
	}
	return nil
}

func validateLicenses(ctx context.Context, rt runtime.Runtime, sink output.Sink, platformClient api.PlatformAPI, containers []runtime.ContainerConfig, cfgContainers []config.ContainerConfig, token string) error {
	for i, c := range cfgContainers {
		if err := validateLicense(ctx, rt, sink, platformClient, containers[i], &c, token); err != nil {
			return err
		}
	}
	return nil
}

func startContainers(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []runtime.ContainerConfig) error {
	for _, c := range containers {
		output.EmitStatus(sink, "starting", c.Name, "")
		containerID, err := rt.Start(ctx, c)
		if err != nil {
			return fmt.Errorf("failed to start %s: %w", c.Name, err)
		}

		output.EmitStatus(sink, "waiting", c.Name, "")
		healthURL := fmt.Sprintf("http://localhost:%s%s", c.Port, c.HealthPath)
		if err := awaitStartup(ctx, rt, sink, containerID, c.Name, healthURL); err != nil {
			return err
		}

		output.EmitStatus(sink, "ready", c.Name, fmt.Sprintf("containerId: %s", containerID[:12]))
	}
	return nil
}

func selectContainersToStart(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []runtime.ContainerConfig, cfgContainers []config.ContainerConfig) ([]runtime.ContainerConfig, []config.ContainerConfig, error) {
	var filtered []runtime.ContainerConfig
	var filteredCfg []config.ContainerConfig
	for i, c := range containers {
		running, err := rt.IsRunning(ctx, c.Name)
		if err != nil && !errdefs.IsNotFound(err) {
			return nil, nil, fmt.Errorf("failed to check container status: %w", err)
		}
		if running {
			output.EmitLog(sink, fmt.Sprintf("%s is already running", c.Name))
			continue
		}
		if err := checkPortAvailable(c.Port); err != nil {
			configPath, pathErr := config.ConfigFilePath()
			if pathErr != nil {
				return nil, nil, err
			}
			return nil, nil, fmt.Errorf("%w\nTo use a different port, edit %s", err, configPath)
		}
		filtered = append(filtered, c)
		filteredCfg = append(filteredCfg, cfgContainers[i])
	}
	return filtered, filteredCfg, nil
}

func checkPortAvailable(port string) error {
	conn, err := net.DialTimeout("tcp", "localhost:"+port, time.Second)
	if err != nil {
		return nil
	}
	err = conn.Close()
	if err != nil {
		return nil
	}
	return fmt.Errorf("port %s already in use", port)
}

func validateLicense(ctx context.Context, rt runtime.Runtime, sink output.Sink, platformClient api.PlatformAPI, containerConfig runtime.ContainerConfig, cfgContainer *config.ContainerConfig, token string) error {
	version := cfgContainer.Tag
	if version == "" || version == "latest" {
		actualVersion, err := rt.GetImageVersion(ctx, containerConfig.Image)
		if err != nil {
			return fmt.Errorf("could not resolve version from image %s: %w", containerConfig.Image, err)
		}
		version = actualVersion
	}

	productName, err := cfgContainer.ProductName()
	if err != nil {
		return err
	}
	output.EmitStatus(sink, "validating license", containerConfig.Name, version)

	hostname, _ := os.Hostname()
	licenseReq := &api.LicenseRequest{
		Product: api.ProductInfo{
			Name:    productName,
			Version: version,
		},
		Credentials: api.CredentialsInfo{
			Token: token,
		},
		Machine: api.MachineInfo{
			Hostname:        hostname,
			Platform:        stdruntime.GOOS,
			PlatformRelease: stdruntime.GOARCH,
		},
	}

	if err := platformClient.GetLicense(ctx, licenseReq); err != nil {
		return fmt.Errorf("license validation failed for %s:%s: %w", productName, version, err)
	}

	return nil
}

// awaitStartup polls until one of two outcomes:
//   - Success: health endpoint returns 200 (license is valid, LocalStack is ready)
//   - Failure: container stops running (e.g., license activation failed), returns error with container logs
//
// TODO: move to Runtime interface if other runtimes (k8s?) need native readiness probes
func awaitStartup(ctx context.Context, rt runtime.Runtime, sink output.Sink, containerID, name, healthURL string) error {
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
			if err := resp.Body.Close(); err != nil {
				output.EmitWarning(sink, fmt.Sprintf("failed to close response body: %v", err))
			}
			return nil
		}
		if resp != nil {
			if err := resp.Body.Close(); err != nil {
				output.EmitWarning(sink, fmt.Sprintf("failed to close response body: %v", err))
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}
