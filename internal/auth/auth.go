package auth

import (
	"context"
	"log"
	"os"
)

// GetToken tries in order: 1) keychain 2) LOCALSTACK_AUTH_TOKEN env var 3) browser login
func GetToken(ctx context.Context) (string, error) {
	// TODO: try keychain first

	if token := os.Getenv("LOCALSTACK_AUTH_TOKEN"); token != "" {
		return token, nil
	}

	log.Println("Authentication required. Opening browser...")
	token, err := Login(ctx)
	if err != nil {
		log.Println("Authentication failed.")
		return "", err
	}
	log.Println("Login successful.")
	return token, nil
}
