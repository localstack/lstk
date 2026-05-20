package azureconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/localstack/lstk/internal/azurecli"
	"github.com/localstack/lstk/internal/output"
)

const (
	CloudName        = "LocalStack"
	DefaultCloudName = "AzureCloud"
	AzureSubdomain   = "azure"

	// Dummy service principal credentials. The LocalStack Azure emulator does
	// not validate these — any values that look like a service principal login
	// are accepted.
	servicePrincipalUser   = "any-app"
	servicePrincipalPass   = "any-pass"
	servicePrincipalTenant = "anytenant"
)

// BuildEndpoint returns the LocalStack Azure endpoint URL for the given host:port.
// The endpoint must use the "azure." subdomain so the LocalStack proxy routes
// requests to the Azure backend.
func BuildEndpoint(host string) string {
	return "https://" + AzureSubdomain + "." + host
}

// IsRunning probes the LocalStack health endpoint at the given Azure endpoint.
func IsRunning(ctx context.Context, endpointURL string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/azureconfig").Start(ctx, "azureconfig.IsRunning")
	defer span.End()

	url := strings.TrimRight(endpointURL, "/") + "/_localstack/health"
	span.SetAttributes(attribute.String("azure.health_url", url))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// cloudExists reports whether the LocalStack cloud is already registered with `az`.
func cloudExists(ctx context.Context) (bool, error) {
	stdout, _, err := azurecli.Run(ctx, "cloud", "list", "--query", "[].name", "--output", "json")
	if err != nil {
		return false, err
	}
	var names []string
	if err := json.Unmarshal([]byte(stdout), &names); err != nil {
		return false, fmt.Errorf("parsing az cloud list output: %w", err)
	}
	for _, n := range names {
		if n == CloudName {
			return true, nil
		}
	}
	return false, nil
}

// buildCloudConfig produces the --cloud-config JSON payload for `az cloud register|update`.
// Trailing slashes match the format produced by the official Azure cloud config so
// that az appends paths without producing concatenated URLs.
func buildCloudConfig(endpointURL string) ([]byte, error) {
	trimmed := strings.TrimRight(endpointURL, "/")
	withSlash := trimmed + "/"
	cfg := struct {
		Endpoints map[string]string `json:"endpoints"`
	}{
		Endpoints: map[string]string{
			"activeDirectory":                trimmed,
			"activeDirectoryResourceId":      trimmed,
			"activeDirectoryGraphResourceId": trimmed,
			"management":                     withSlash,
			"microsoftGraphResourceId":       withSlash,
			"resourceManager":                withSlash,
			"logAnalyticsResourceId":         trimmed,
		},
	}
	return json.Marshal(cfg)
}

// registerOrUpdate registers (or updates if it already exists) the LocalStack cloud.
func registerOrUpdate(ctx context.Context, endpointURL string, alreadyExists bool) error {
	verb := "register"
	if alreadyExists {
		verb = "update"
	}
	cloudConfigJSON, err := buildCloudConfig(endpointURL)
	if err != nil {
		return err
	}
	_, _, err = azurecli.Run(ctx, "cloud", verb,
		"--name", CloudName,
		"--cloud-config", string(cloudConfigJSON),
	)
	return err
}

func setActiveCloud(ctx context.Context, name string) error {
	_, _, err := azurecli.Run(ctx, "cloud", "set", "--name", name, "--only-show-errors")
	return err
}

func disableInstanceDiscovery(ctx context.Context) error {
	_, _, err := azurecli.Run(ctx, "config", "set", "core.instance_discovery=false", "--only-show-errors")
	return err
}

func loginServicePrincipal(ctx context.Context) error {
	_, _, err := azurecli.Run(ctx, "login", "--service-principal",
		"-u", servicePrincipalUser,
		"-p", servicePrincipalPass,
		"--tenant", servicePrincipalTenant,
		"--only-show-errors",
	)
	return err
}

// Setup runs the full Azure custom cloud setup flow against the given LocalStack
// Azure endpoint. It expects the emulator to be running and prompts the user for
// confirmation before mutating the local Azure CLI configuration.
func Setup(ctx context.Context, sink output.Sink, endpointURL string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/azureconfig").Start(ctx, "azureconfig.Setup")
	defer span.End()

	if err := IsRunning(ctx, endpointURL); err != nil {
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityWarning,
			Text: fmt.Sprintf(
				"LocalStack Azure emulator not reachable at %s. Start it with 'lstk start' before running 'lstk setup azure'.",
				endpointURL,
			),
		})
		return fmt.Errorf("emulator not reachable at %s: %w", endpointURL, err)
	}

	responseCh := make(chan output.InputResponse, 1)
	sink.Emit(output.UserInputRequestEvent{
		Prompt:     "Set up the LocalStack Azure cloud in `az`?",
		Options:    []output.InputOption{{Key: "y", Label: "Y"}, {Key: "n", Label: "n"}},
		ResponseCh: responseCh,
	})

	select {
	case resp := <-responseCh:
		if resp.Cancelled {
			return nil
		}
		if resp.SelectedKey == "n" {
			sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Skipped Azure cloud setup."})
			return nil
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	exists, err := cloudExists(ctx)
	if err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not list Azure clouds: %v", err)})
		return err
	}

	verb := "Registering"
	if exists {
		verb = "Updating"
	}
	sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("%s %q cloud...", verb, CloudName)})
	if err := registerOrUpdate(ctx, endpointURL, exists); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not register Azure cloud: %v", err)})
		return err
	}

	sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: "Activating cloud and disabling instance discovery..."})
	if err := setActiveCloud(ctx, CloudName); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not activate Azure cloud: %v", err)})
		return err
	}
	if err := disableInstanceDiscovery(ctx); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not disable instance discovery: %v", err)})
		return err
	}

	sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: "Logging in with dummy service-principal credentials..."})
	if err := loginServicePrincipal(ctx); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not log in to Azure CLI: %v", err)})
		return err
	}

	sink.Emit(output.MessageEvent{
		Severity: output.SeveritySuccess,
		Text:     fmt.Sprintf("Set up %q Azure cloud at %s.", CloudName, endpointURL),
	})
	return nil
}
