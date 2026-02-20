package env

import (
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Env struct {
	AuthToken   string
	APIEndpoint string
	WebAppURL   string
	Keyring     string
}

var Vars = &Env{}

// Init initializes environment variable configuration
func Init() {
	viper.SetEnvPrefix("LSTK")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("api_endpoint", "https://api.localstack.cloud")
	viper.SetDefault("web_app_url", "https://app.localstack.cloud")

	// LOCALSTACK_AUTH_TOKEN is not prefixed with LSTK_
	// in order to be shared seamlessly with other LocalStack tools
	Vars = &Env{
		AuthToken:   os.Getenv("LOCALSTACK_AUTH_TOKEN"),
		APIEndpoint: viper.GetString("api_endpoint"),
		WebAppURL:   viper.GetString("web_app_url"),
		Keyring:     viper.GetString("keyring"),
	}
}
