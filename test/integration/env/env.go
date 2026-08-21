package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type Key string

const (
	AuthToken         Key = "LOCALSTACK_AUTH_TOKEN"
	LocalStackHost    Key = "LOCALSTACK_HOST"
	APIEndpoint       Key = "LSTK_API_ENDPOINT"
	WebAppURL         Key = "LSTK_WEB_APP_URL"
	Keyring           Key = "LSTK_KEYRING"
	CI                Key = "CI"
	AnalyticsEndpoint Key = "LSTK_ANALYTICS_ENDPOINT"
	DisableEvents     Key = "LOCALSTACK_DISABLE_EVENTS"
	Home              Key = "HOME"
	UserProfile       Key = "USERPROFILE"
	Path              Key = "PATH"
	Persistence       Key = "LOCALSTACK_PERSISTENCE"
	Otel              Key = "LSTK_OTEL"
	OtelEndpoint      Key = "OTEL_EXPORTER_OTLP_ENDPOINT"
	StartupTimeout    Key = "LSTK_STARTUP_TIMEOUT"
	// UpdateCheck overrides the [cli] update_check policy: "prompt", "notify" or
	// "off".
	UpdateCheck Key = "LSTK_UPDATE_CHECK"
	// UpdateGitHubAPIEndpoint and UpdateGitHubDownloadEndpoint point the
	// updater's release-metadata API (api.github.com) and asset downloads
	// (github.com) at mock servers (undocumented, test-only).
	UpdateGitHubAPIEndpoint      Key = "LSTK_UPDATE_GITHUB_API_ENDPOINT"
	UpdateGitHubDownloadEndpoint Key = "LSTK_UPDATE_GITHUB_DOWNLOAD_ENDPOINT"
	// BrowserCmd replaces the OS browser launcher for the login flow
	// (undocumented, test-only): on Windows pkg/browser opens URLs via the
	// ShellExecute Win32 call, which fake binaries on PATH cannot intercept.
	BrowserCmd         Key = "LSTK_BROWSER_CMD"
	AWSAccessKeyID     Key = "AWS_ACCESS_KEY_ID"
	AWSSecretAccessKey Key = "AWS_SECRET_ACCESS_KEY"
	// AzureCollectTelemetry controls the Azure CLI's usage telemetry. Defaulted to
	// "false" in every test environment: an enabled `az` spawns a background uploader
	// that keeps a handle on the test's temp dir, breaking t.TempDir() cleanup on Windows.
	AzureCollectTelemetry Key = "AZURE_CORE_COLLECT_TELEMETRY"
	// SamCliTelemetry controls the AWS SAM CLI's usage telemetry. Defaulted to
	// "0" in every test environment: on a fresh (isolated) home, sam's
	// first-run telemetry path prints an opt-out notice and phones home, which
	// made `sam validate` in TestSAME2EValidateOffline hang for minutes and
	// exit non-zero on the Windows runner.
	SamCliTelemetry Key = "SAM_CLI_TELEMETRY"
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

// ambientAWSKeys are the AWS credential/config variables lstk and the wrapped
// tools resolve from the environment. They are stripped from every test
// environment so the developer's real shell (profile, region, keys, endpoint
// overrides) can't leak into wrapped-tool env dumps, credential resolution, or
// snapshotted output; tests exercising ambient behavior set them explicitly
// via With, which wins because exec dedups duplicate keys to the last value.
var ambientAWSKeys = []Key{
	"AWS_PROFILE", "AWS_DEFAULT_PROFILE",
	"AWS_REGION", "AWS_DEFAULT_REGION",
	"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
	"AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_S3",
	"LSTK_ENDPOINT_URL",
}

func base() Environ {
	return Environ(os.Environ()).
		Without(ambientAWSKeys...).
		With(AnalyticsEndpoint, UnreachableAnalyticsEndpoint).
		With(AzureCollectTelemetry, "false").
		With(SamCliTelemetry, "0")
}

func Without(keys ...Key) Environ {
	return base().Without(keys...)
}

func With(key Key, value string) Environ {
	return base().With(key, value)
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

// WithHome points the process home directory at dir on every OS: HOME (what
// Unix and most tools read), plus USERPROFILE (what os.UserHomeDir reads on
// Windows) and APPDATA (os.UserConfigDir on Windows), so the binary under
// test resolves the same isolated home everywhere. Setting only HOME leaves a
// Windows binary pointed at the real user profile.
func (e Environ) WithHome(dir string) Environ {
	return e.With(Home, dir).
		With(UserProfile, dir).
		With(Key("APPDATA"), filepath.Join(dir, "AppData", "Roaming"))
}

// WithHome is the package-level variant of Environ.WithHome, starting from
// the current process environment (with the usual test-safe analytics and
// Azure-telemetry defaults applied).
func WithHome(dir string) Environ {
	return Without().WithHome(dir)
}
