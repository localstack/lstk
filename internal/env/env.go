package env

import (
	"os"
	"strings"

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
func Init() *Env {
	viper.SetEnvPrefix("LSTK")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("api_endpoint", "https://api.localstack.cloud")
	viper.SetDefault("web_app_url", "https://app.localstack.cloud")
	viper.SetDefault("analytics_endpoint", "https://analytics.localstack.cloud/v1/events")
	// LOCALSTACK_AUTH_TOKEN and LOCALSTACK_DISABLE_EVENTS are not prefixed with LSTK_
	// so they work seamlessly across all LocalStack tools without per-tool configuration
	return &Env{
		AuthToken:         os.Getenv("LOCALSTACK_AUTH_TOKEN"),
		APIEndpoint:       viper.GetString("api_endpoint"),
		WebAppURL:         viper.GetString("web_app_url"),
		ForceFileKeyring:  viper.GetString("keyring") == "file",
		AnalyticsEndpoint: viper.GetString("analytics_endpoint"),
		DisableEvents:     os.Getenv("LOCALSTACK_DISABLE_EVENTS") == "1",
	}

}
