package env

import (
	"fmt"
	"os"
	"strings"

	"github.com/localstack/lstk/internal/validate"
	"github.com/spf13/viper"
)

type Env struct {
	AuthToken         string
	APIEndpoint       string
	WebAppURL         string
	ForceFileKeyring  bool
	AnalyticsEndpoint string
	DisableEvents     bool
}

// Init initializes environment variable configuration and returns the result.
func Init() (*Env, error) {
	viper.SetEnvPrefix("LSTK")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("api_endpoint", "https://api.localstack.cloud")
	viper.SetDefault("web_app_url", "https://app.localstack.cloud")
	viper.SetDefault("analytics_endpoint", "https://analytics.localstack.cloud/v1/events")
	// LOCALSTACK_AUTH_TOKEN and LOCALSTACK_DISABLE_EVENTS are not prefixed with LSTK_
	// so they work seamlessly across all LocalStack tools without per-tool configuration
	authToken := os.Getenv("LOCALSTACK_AUTH_TOKEN")
	if err := validate.NoControlChars("LOCALSTACK_AUTH_TOKEN", authToken); err != nil {
		return nil, err
	}

	apiEndpoint := viper.GetString("api_endpoint")
	if err := validate.HTTPSURL("LSTK_API_ENDPOINT", apiEndpoint); err != nil {
		return nil, fmt.Errorf("invalid api endpoint: %w", err)
	}

	webAppURL := viper.GetString("web_app_url")
	if err := validate.HTTPSURL("LSTK_WEB_APP_URL", webAppURL); err != nil {
		return nil, fmt.Errorf("invalid web app URL: %w", err)
	}

	analyticsEndpoint := viper.GetString("analytics_endpoint")
	if err := validate.HTTPSURL("LSTK_ANALYTICS_ENDPOINT", analyticsEndpoint); err != nil {
		return nil, fmt.Errorf("invalid analytics endpoint: %w", err)
	}

	return &Env{
		AuthToken:         authToken,
		APIEndpoint:       apiEndpoint,
		WebAppURL:         webAppURL,
		ForceFileKeyring:  viper.GetString("keyring") == "file",
		AnalyticsEndpoint: analyticsEndpoint,
		DisableEvents:     os.Getenv("LOCALSTACK_DISABLE_EVENTS") == "1",
	}, nil
}
