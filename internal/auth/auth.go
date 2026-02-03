package auth

import (
	"errors"
	"os"
)

// GetToken returns the auth token from keyring or environment variable.
func GetToken() (string, error) {
	// TODO: try keyring first

	if token := os.Getenv("LOCALSTACK_AUTH_TOKEN"); token != "" {
		return token, nil
	}
	return "", errors.New("auth token not found, please set LOCALSTACK_AUTH_TOKEN")
}
