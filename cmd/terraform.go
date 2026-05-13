package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	terraformcli "github.com/localstack/lstk/internal/iac/terraform/cli"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

func newTerraformCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "terraform [args...]",
		Aliases: []string{"tf"},
		Short:   "Run Terraform commands against LocalStack",
		Long: `Deploy a Terraform project to LocalStack, wrapping the standard terraform command.

Examples:
  lstk terraform init
  lstk terraform plan
  lstk tf apply -auto-approve`,
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

			awsContainer := config.ContainerConfig{Type: config.EmulatorAWS, Port: config.DefaultAWSPort}
			for _, c := range appCfg.Containers {
				if c.Type == config.EmulatorAWS {
					awsContainer = c
					break
				}
			}

			sink := output.NewPlainSink(os.Stdout)

			if err := rt.IsHealthy(cmd.Context()); err != nil {
				rt.EmitUnhealthyError(sink, err)
				return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
			}

			runningName, err := container.ResolveRunningContainerName(cmd.Context(), rt, awsContainer)
			if err != nil {
				return fmt.Errorf("checking emulator status: %w", err)
			}
			if runningName == "" {
				sink.Emit(output.ErrorEvent{
					Title: fmt.Sprintf("%s is not running", awsContainer.DisplayName()),
					Actions: []output.ErrorAction{
						{Label: "Start LocalStack:", Value: "lstk"},
						{Label: "See help:", Value: "lstk -h"},
					},
				})
				return output.NewSilentError(fmt.Errorf("%s is not running", awsContainer.Name()))
			}

			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
			defaultEndpoint := "http://" + host

			opts := terraformcli.Options{
				Endpoints:                  buildTerraformEndpoints(os.Environ(), defaultEndpoint),
				TerraformBin:               os.Getenv("TF_CMD"),
				AccessKey:                  os.Getenv("AWS_ACCESS_KEY_ID"),
				Region:                     os.Getenv("AWS_DEFAULT_REGION"),
				CustomizeAccessKey:         envBool("CUSTOMIZE_ACCESS_KEY"),
				AutoCreateBackendResources: true,
			}

			return terraformcli.Exec(cmd.Context(), opts, os.Stdin, os.Stdout, os.Stderr, args)
		},
	}
}

// buildTerraformEndpoints assembles the canonical Endpoints slice passed to
// the terraform CLI wrapper. It scans environ for AWS_ENDPOINT_URL[_*]
// values and resolves precedence:
//   - default endpoint:   user-supplied AWS_ENDPOINT_URL, else
//     lstk's resolved LocalStack base.
//   - S3 endpoint (Service "S3"):       user-supplied AWS_ENDPOINT_URL_S3,
//     else derived virtual-hosted host from the chosen base.
//   - any other AWS_ENDPOINT_URL_<SVC>: passed through verbatim only if the
//     user provided it; lstk never invents a default for other services.
//
// Ordering: default first, then S3, then remaining services alphabetically
// (for deterministic test output and OTel attribute readability).
func buildTerraformEndpoints(environ []string, defaultEndpoint string) []terraformcli.Endpoint {
	userProvidedEndpoint := map[string]string{}
	for _, kv := range environ {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			continue
		}
		key, val := kv[:idx], kv[idx+1:]
		if val == "" {
			continue
		}
		switch {
		case key == "AWS_ENDPOINT_URL":
			userProvidedEndpoint[""] = val
		case strings.HasPrefix(key, "AWS_ENDPOINT_URL_"):
			svc := strings.TrimPrefix(key, "AWS_ENDPOINT_URL_")
			if svc != "" {
				userProvidedEndpoint[svc] = val
			}
		}
	}

	baseEndpoint := userProvidedEndpoint[""]
	if baseEndpoint == "" {
		baseEndpoint = defaultEndpoint
	}
	out := []terraformcli.Endpoint{{Service: "", URL: baseEndpoint}}

	s3Endpoint := userProvidedEndpoint["S3"]
	if s3Endpoint == "" {
		s3Endpoint = endpoint.DeriveS3Endpoint(baseEndpoint)
	}
	out = append(out, terraformcli.Endpoint{Service: "S3", URL: s3Endpoint})

	others := make([]string, 0, len(userProvidedEndpoint))
	for svc := range userProvidedEndpoint {
		if svc == "" || svc == "S3" {
			continue
		}
		others = append(others, svc)
	}
	sort.Strings(others)
	for _, svc := range others {
		out = append(out, terraformcli.Endpoint{Service: svc, URL: userProvidedEndpoint[svc]})
	}
	return out
}

// envBool returns true for the typical truthy values that tflocal accepts.
func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
