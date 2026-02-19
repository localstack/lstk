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

// GetToken tries in order: 1) keyring 2) LSTK_AUTH_TOKEN env var 3) device flow login
func (a *Auth) GetToken(ctx context.Context) (string, error) {
	if token, err := a.tokenStorage.GetAuthToken(); err == nil && token != "" {
		return token, nil
	}

	if token := env.Vars.AuthToken; token != "" {
		return token, nil
	}

	if !a.allowLogin {
		return "", fmt.Errorf("authentication required: set LSTK_AUTH_TOKEN or run in interactive mode")
	}

	output.EmitLog(a.sink, "No existing credentials found. Please log in:")
	token, err := a.login.Login(ctx)
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

// LogoutResult represents the outcome of a logout operation
type LogoutResult struct {
	TokenDeleted bool
}

// Logout removes the stored auth token from the keyring.
// Returns LogoutResult indicating whether a token was actually deleted.
func (a *Auth) Logout() (LogoutResult, error) {
	err := a.tokenStorage.DeleteAuthToken()
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return LogoutResult{TokenDeleted: false}, nil
	}
	if err != nil {
		return LogoutResult{}, err
	}
	return LogoutResult{TokenDeleted: true}, nil
}
