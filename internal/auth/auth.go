package auth

import (
	"context"
	"log"
	"os"
)

type Auth struct {
	keyring      Keyring
	browserLogin LoginProvider
}

func New() *Auth {
	return &Auth{
		keyring:      systemKeyring{},
		browserLogin: browserLogin{},
	}
}

// GetToken tries in order: 1) keyring 2) LOCALSTACK_AUTH_TOKEN env var 3) browser login
func (a *Auth) GetToken(ctx context.Context) (string, error) {
	if token, err := a.keyring.Get(keyringService, keyringUser); err == nil && token != "" {
		return token, nil
	}

	if token := os.Getenv("LOCALSTACK_AUTH_TOKEN"); token != "" {
		return token, nil
	}

	log.Println("Authentication required. Opening browser...")
	token, err := a.browserLogin.Login(ctx)
	if err != nil {
		log.Println("Authentication failed.")
		return "", err
	}

	if err := a.keyring.Set(keyringService, keyringUser, token); err != nil {
		log.Printf("Warning: could not store token in keyring: %v", err)
	}

	log.Println("Login successful.")
	return token, nil
}
