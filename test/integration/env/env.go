package env

import (
	"os"
	"strings"
	"testing"
)

type Key string

const (
	AuthToken          Key = "LOCALSTACK_AUTH_TOKEN"
	LocalStackHost     Key = "LOCALSTACK_HOST"
	LocalStackPath     Key = "LOCALSTACK_PATH"
	APIEndpoint        Key = "LSTK_API_ENDPOINT"
	Keyring            Key = "LSTK_KEYRING"
	CI                 Key = "CI"
	AnalyticsEndpoint  Key = "LSTK_ANALYTICS_ENDPOINT"
	DisableEvents      Key = "LOCALSTACK_DISABLE_EVENTS"
	Home               Key = "HOME"
	UserProfile        Key = "USERPROFILE"
	Persistence        Key = "LOCALSTACK_PERSISTENCE"
	Otel               Key = "LSTK_OTEL"
	OtelEndpoint       Key = "OTEL_EXPORTER_OTLP_ENDPOINT"
	AWSAccessKeyID     Key = "AWS_ACCESS_KEY_ID"
	AWSSecretAccessKey Key = "AWS_SECRET_ACCESS_KEY"
	// AzureCollectTelemetry controls the Azure CLI's usage telemetry. Defaulted to
	// "false" in every test environment: an enabled `az` spawns a background uploader
	// that keeps a handle on the test's temp dir, breaking t.TempDir() cleanup on Windows.
	AzureCollectTelemetry Key = "AZURE_CORE_COLLECT_TELEMETRY"
)

// UnreachableAnalyticsEndpoint is a closed local port used as the default
// analytics endpoint for every test environment, so the binary under test never
// reports telemetry to the production analytics backend (which would pollute it,
// e.g. with "start" events tagged as coming from CI or an AI agent). Tests that
// exercise telemetry override it with a mock server URL via With(AnalyticsEndpoint, ...);
// the explicit value wins because exec dedups duplicate keys to the last value.
const UnreachableAnalyticsEndpoint = "http://127.0.0.1:1"

func Get(key Key) string {
	return os.Getenv(string(key))
}

func Require(t testing.TB, key Key) string {
	t.Helper()
	v := os.Getenv(string(key))
	if v == "" {
		t.Fatalf("%s must be set to run this test", key)
	}
	return v
}

type Environ []string

func Without(keys ...Key) Environ {
	return Environ(os.Environ()).
		With(AnalyticsEndpoint, UnreachableAnalyticsEndpoint).
		With(AzureCollectTelemetry, "false").
		Without(keys...)
}

func With(key Key, value string) Environ {
	return Environ(os.Environ()).
		With(AnalyticsEndpoint, UnreachableAnalyticsEndpoint).
		With(AzureCollectTelemetry, "false").
		With(key, value)
}

func (e Environ) Without(keys ...Key) Environ {
	var result Environ
	for _, entry := range e {
		excluded := false
		for _, key := range keys {
			if strings.HasPrefix(entry, string(key)+"=") {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, entry)
		}
	}
	return result
}

func (e Environ) With(key Key, value string) Environ {
	return append(e, string(key)+"="+value)
}
