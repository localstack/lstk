package env

import (
	"strings"

	"github.com/spf13/viper"
)

type Env struct {
	AuthToken   string
	APIEndpoint string
	WebAppURL   string
	Keyring     string
}

var Vars *Env

// Init initializes environment variable configuration
func Init() {
	viper.SetEnvPrefix("LOCALSTACK")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("api_endpoint", "https://api.localstack.cloud")
	viper.SetDefault("web_app_url", "https://app.localstack.cloud")

	Vars = &Env{
		AuthToken:   viper.GetString("auth_token"),
		APIEndpoint: viper.GetString("api_endpoint"),
		WebAppURL:   viper.GetString("web_app_url"),
		Keyring:     viper.GetString("keyring"),
	}
}
