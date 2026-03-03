package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
)

var ErrNotLoggedIn = errors.New("not logged in")

type Auth struct {
	tokenStorage AuthTokenStorage
	login        LoginProvider
	sink         output.Sink
	authToken    string
	allowLogin   bool
}

func New(sink output.Sink, platform api.PlatformAPI, storage AuthTokenStorage, authToken, webAppURL string, allowLogin bool) *Auth {
	return &Auth{
		tokenStorage: storage,
		login:        newLoginProvider(sink, platform, webAppURL),
		sink:         sink,
		authToken:    authToken,
		allowLogin:   allowLogin,
	}
}

// GetToken tries in order: 1) keyring 2) LOCALSTACK_AUTH_TOKEN env var 3) device flow login
func (a *Auth) GetToken(ctx context.Context) (string, error) {
	if token, err := a.tokenStorage.GetAuthToken(); err == nil && token != "" {
		return token, nil
	}

	if token := a.authToken; token != "" {
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
	if err != nil {
		output.EmitSpinnerStop(a.sink)
		if a.authToken != "" {
			output.EmitNote(a.sink, "Authenticated via LOCALSTACK_AUTH_TOKEN environment variable; unset it to log out")
			return nil
		}
		if !errors.Is(err, ErrTokenNotFound) {
			return fmt.Errorf("failed to read auth token: %w", err)
		}
		output.EmitNote(a.sink, "Not currently logged in")
		return ErrNotLoggedIn
	}

	if err := a.tokenStorage.DeleteAuthToken(); err != nil {
		output.EmitSpinnerStop(a.sink)
		return fmt.Errorf("failed to delete auth token: %w", err)
	}

	output.EmitSpinnerStop(a.sink)
	output.EmitSuccess(a.sink, "Logged out successfully")
	return nil
}
