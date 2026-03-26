package env

import (
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Env struct {
	AuthToken      string
	LocalStackHost string
	DockerHost     string
	DisableEvents  bool

	APIEndpoint       string
	WebAppURL         string
	ForceFileKeyring  bool
	AuthTokenFile     string
	AnalyticsEndpoint string

	NonInteractive bool
	GitHubToken    string
	ContainerName  string
}

// Init initializes environment variable configuration and returns the result.
func Init() *Env {
	viper.SetEnvPrefix("LSTK")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("api_endpoint", "https://api.localstack.cloud")
	viper.SetDefault("web_app_url", "https://app.localstack.cloud")
	viper.SetDefault("analytics_endpoint", "https://analytics.localstack.cloud/v1/events")
	// LOCALSTACK_* variables are not prefixed with LSTK_ so they work seamlessly
	// across all LocalStack tools without per-tool configuration
	return &Env{
		AuthToken:         os.Getenv("LOCALSTACK_AUTH_TOKEN"),
		LocalStackHost:    os.Getenv("LOCALSTACK_HOST"),
		DockerHost:        os.Getenv("DOCKER_HOST"),
		DisableEvents:     os.Getenv("LOCALSTACK_DISABLE_EVENTS") == "1",
		APIEndpoint:       viper.GetString("api_endpoint"),
		WebAppURL:         viper.GetString("web_app_url"),
		ForceFileKeyring:  viper.GetString("keyring") == "file",
		AuthTokenFile:     viper.GetString("auth_token_file"),
		AnalyticsEndpoint: viper.GetString("analytics_endpoint"),
		GitHubToken:       viper.GetString("github_token"),
		ContainerName:     viper.GetString("container_name"),
	}

}
