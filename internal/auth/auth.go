package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/99designs/keyring"
	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
)

type Auth struct {
	tokenStorage AuthTokenStorage
	login        LoginProvider
	sink         output.Sink
	allowLogin   bool
}

func New(sink output.Sink, platform api.PlatformAPI, storage AuthTokenStorage, allowLogin bool) *Auth {
	return &Auth{
		tokenStorage: storage,
		login:        newLoginProvider(sink, platform),
		sink:         sink,
		allowLogin:   allowLogin,
	}
}

// GetToken tries in order: 1) keyring 2) LOCALSTACK_AUTH_TOKEN env var 3) device flow login
func (a *Auth) GetToken(ctx context.Context) (string, error) {
	if token, err := a.tokenStorage.GetAuthToken(); err == nil && token != "" {
		return token, nil
	}

	if token := env.Vars.AuthToken; token != "" {
		return token, nil
	}

	if !a.allowLogin {
		return "", fmt.Errorf("authentication required: set LOCALSTACK_AUTH_TOKEN or run in interactive mode")
	}

	output.EmitSecondaryLog(a.sink, "> Welcome to LSTK, a command-line interface for LocalStack")
	output.EmitLog(a.sink, "")
	token, err := a.login.Login(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", err
		}
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
