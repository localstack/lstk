package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
)

var ErrNotLoggedIn = errors.New("not logged in")

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

	output.EmitInfo(a.sink, "No existing credentials found. Please log in:")
	token, err := a.login.Login(ctx)
	if err != nil {
		output.EmitWarning(a.sink, "Authentication failed.")
		return "", err
	}

	if err := a.tokenStorage.SetAuthToken(token); err != nil {
		output.EmitWarning(a.sink, fmt.Sprintf("could not store token in keyring: %v", err))
	}

	output.EmitSuccess(a.sink, "Login successful.")
	return token, nil
}

// Logout removes the stored auth token from the keyring
func (a *Auth) Logout() error {
	output.EmitSpinnerStart(a.sink, "Logging out...")

	_, err := a.tokenStorage.GetAuthToken()
	if errors.Is(err, ErrTokenNotFound) {
		output.EmitSpinnerStop(a.sink)
		output.EmitNote(a.sink, "Not currently logged in")
		return ErrNotLoggedIn
	}
	if err != nil {
		output.EmitSpinnerStop(a.sink)
		return fmt.Errorf("failed to read auth token: %w", err)
	}

	if err := a.tokenStorage.DeleteAuthToken(); err != nil {
		output.EmitSpinnerStop(a.sink)
		return fmt.Errorf("failed to delete auth token: %w", err)
	}

	output.EmitSpinnerStop(a.sink)
	output.EmitSuccess(a.sink, "Logged out successfully")
	return nil
}
