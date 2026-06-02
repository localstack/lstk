package azureconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/localstack/lstk/internal/azurecli"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
)

const (
	AzureSubdomain = "azure"
	CloudName      = "LocalStack"

	// setupMarkerFile is written at the end of a successful Setup so partial failures
	// (e.g. login crash after cloud registration) don't make IsSetUp report true.
	setupMarkerFile = ".lstk-setup-complete"

	// Dummy service principal credentials. The LocalStack Azure emulator does
	// not validate these — any values that look like a service principal login
	// are accepted.
	servicePrincipalUser   = "any-app"
	servicePrincipalPass   = "any-pass"
	servicePrincipalTenant = "anytenant"
)

func ConfigDir(lstkConfigDir string) string {
	return filepath.Join(lstkConfigDir, "azure")
}

func BuildEndpoint(host string) string {
	return "https://" + AzureSubdomain + "." + host
}

func IsHealthy(ctx context.Context, endpointURL string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/azureconfig").Start(ctx, "azureconfig.IsHealthy")
	defer span.End()

	url := strings.TrimRight(endpointURL, "/") + "/_localstack/health"
	span.SetAttributes(attribute.String("azure.health_url", url))

	resp, err := httpGet(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func Env(azureConfigDir string) []string {
	return []string{"AZURE_CONFIG_DIR=" + azureConfigDir}
}

func IsSetUp(azureConfigDir string) bool {
	_, err := os.Stat(filepath.Join(azureConfigDir, setupMarkerFile))
	return err == nil
}

func httpGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req)
}

// BuildCloudConfig returns the JSON payload for `az cloud register/update --cloud-config`.
func BuildCloudConfig(endpointURL string) (string, error) {
	base := strings.TrimRight(endpointURL, "/")
	payload := map[string]any{
		"endpoints": map[string]string{
			"activeDirectory":                base,
			"activeDirectoryResourceId":      base,
			"activeDirectoryGraphResourceId": base,
			"management":                     base + "/",
			"microsoftGraphResourceId":       base + "/",
			"resourceManager":                base + "/",
			"logAnalyticsResourceId":         base,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func cloudExists(ctx context.Context, azEnv []string) (bool, error) {
	stdout, _, err := azurecli.Run(ctx, azEnv,
		"cloud", "list", "--query", fmt.Sprintf("[?name=='%s'].name", CloudName), "-o", "tsv")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(stdout) == CloudName, nil
}

// RunSetup derives the LocalStack Azure endpoint from the configured containers
// and runs Setup against the isolated Azure CLI config dir under lstkConfigDir.
// It works with any sink, so it serves both the interactive (TUI) and
// non-interactive (plain) paths.
func RunSetup(ctx context.Context, sink output.Sink, containers []config.ContainerConfig, localStackHost, lstkConfigDir string) error {
	var azureContainer *config.ContainerConfig
	for i := range containers {
		if containers[i].Type == config.EmulatorAzure {
			azureContainer = &containers[i]
			break
		}
	}
	if azureContainer == nil {
		return fmt.Errorf("no azure emulator configured — run 'lstk start' and select the Azure emulator first")
	}

	resolvedHost, dnsOK := endpoint.ResolveHost(ctx, azureContainer.Port, localStackHost)
	if !dnsOK {
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityWarning,
			Text: fmt.Sprintf(
				"Could not resolve *.%s to 127.0.0.1. Azure setup requires DNS resolution because the Azure emulator serves endpoints under *.%s. Configure DNS or set LOCALSTACK_HOST.",
				endpoint.Hostname, endpoint.Hostname,
			),
		})
		return fmt.Errorf("dns resolution required for azure setup")
	}

	return Setup(ctx, sink, BuildEndpoint(resolvedHost), ConfigDir(lstkConfigDir))
}

// Setup registers the LocalStack custom cloud in an isolated AZURE_CONFIG_DIR,
// activates it, disables instance discovery, and logs in with a dummy SP.
func Setup(ctx context.Context, sink output.Sink, endpointURL, azureConfigDir string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/azureconfig").Start(ctx, "azureconfig.Setup")
	defer span.End()

	// Bail early if `az` is missing so we don't leave a half-configured dir behind.
	if err := azurecli.CheckInstalled(); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: err.Error()})
		return err
	}

	if err := IsHealthy(ctx, endpointURL); err != nil {
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityWarning,
			Text:     fmt.Sprintf("LocalStack Azure emulator not reachable at %s. Run 'lstk' to start it before running 'lstk setup azure'.", endpointURL),
		})
		return fmt.Errorf("emulator not reachable at %s: %w", endpointURL, err)
	}

	if err := os.MkdirAll(azureConfigDir, 0700); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not create %s: %v", azureConfigDir, err)})
		return err
	}
	azEnv := Env(azureConfigDir)

	cloudConfigJSON, err := BuildCloudConfig(endpointURL)
	if err != nil {
		return fmt.Errorf("building cloud config: %w", err)
	}

	exists, err := cloudExists(ctx, azEnv)
	if err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not list Azure clouds: %v", err)})
		return err
	}
	action, verb := "register", "Registering"
	if exists {
		action, verb = "update", "Updating"
	}
	sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("%s '%s' custom cloud...", verb, CloudName)})
	if _, _, err := azurecli.Run(ctx, azEnv,
		"cloud", action, "--name", CloudName, "--cloud-config", cloudConfigJSON, "--only-show-errors"); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not %s '%s' cloud: %v", action, CloudName, err)})
		return err
	}

	if _, _, err := azurecli.Run(ctx, azEnv, "cloud", "set", "--name", CloudName, "--only-show-errors"); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not activate '%s' cloud: %v", CloudName, err)})
		return err
	}

	// instance_discovery=false: `az` would otherwise try to validate the authority
	// against the public AAD discovery endpoint, which the emulator can't serve.
	if _, _, err := azurecli.Run(ctx, azEnv, "config", "set",
		"core.instance_discovery=false", "core.collect_telemetry=false", "output.show_survey_link=no",
		"--only-show-errors"); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not configure Azure CLI: %v", err)})
		return err
	}

	sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: "Logging in with dummy service-principal credentials..."})
	if _, _, err := azurecli.Run(ctx, azEnv, "login", "--service-principal",
		"-u", servicePrincipalUser,
		"-p", servicePrincipalPass,
		"--tenant", servicePrincipalTenant,
		"--only-show-errors",
	); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not log in to the LocalStack Azure emulator: %v", err)})
		return err
	}

	if err := os.WriteFile(filepath.Join(azureConfigDir, setupMarkerFile), []byte("ok\n"), 0600); err != nil {
		return fmt.Errorf("writing setup marker: %w", err)
	}

	sink.Emit(output.MessageEvent{
		Severity: output.SeveritySuccess,
		Text:     "Azure CLI integration ready. Run 'lstk az <command>' to talk to LocalStack.",
	})
	return nil
}
