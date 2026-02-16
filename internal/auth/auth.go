package auth

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/99designs/keyring"
	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
)

type Auth struct {
	tokenStorage AuthTokenStorage
	browserLogin LoginProvider
	sink         output.Sink
}

func New(sink output.Sink, platformClient api.PlatformAPI) (*Auth, error) {
	storage, err := newAuthTokenStorage()
	if err != nil {
		return nil, err
	}
	return &Auth{
		tokenStorage: storage,
		browserLogin: newBrowserLogin(sink, platformClient),
		sink:         sink,
	}, nil
}

// GetToken tries in order: 1) keyring 2) LOCALSTACK_AUTH_TOKEN env var 3) browser login
func (a *Auth) GetToken(ctx context.Context) (string, error) {
	if token, err := a.tokenStorage.GetAuthToken(); err == nil && token != "" {
		return token, nil
	}

	if token := os.Getenv("LOCALSTACK_AUTH_TOKEN"); token != "" {
		return token, nil
	}

	output.EmitLog(a.sink, "Authentication required. Opening browser...")
	token, err := a.browserLogin.Login(ctx)
	if err != nil {
		output.EmitWarning(a.sink, "Authentication failed.")
		return "", err
	}

	if err := a.tokenStorage.SetAuthToken(token); err != nil {
		output.EmitWarning(a.sink, fmt.Sprintf("could not store token in keyring: %v", err))
	}

	output.EmitLog(a.sink, "Login successful.")
	return token, nil
}

// Logout removes the stored auth token from the keyring
func (a *Auth) Logout() error {
	err := a.tokenStorage.DeleteAuthToken()
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil
	}
	return err
}
