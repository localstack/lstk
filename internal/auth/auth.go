package auth

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
)

var ErrNotLoggedIn = errors.New("not logged in")

type Auth struct {
	tokenStorage    AuthTokenStorage
	login           LoginProvider
	sink            output.Sink
	authToken       string
	allowLogin      bool
	licenseFilePath string
}

func New(sink output.Sink, platform api.PlatformAPI, storage AuthTokenStorage, authToken, webAppURL string, allowLogin bool, licenseFilePath string) *Auth {
	return &Auth{
		tokenStorage:    storage,
		login:           newLoginProvider(sink, platform, webAppURL),
		sink:            sink,
		authToken:       authToken,
		allowLogin:      allowLogin,
		licenseFilePath: licenseFilePath,
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

	token, err := a.login.Login(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", err
		}
		a.sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: "Authentication failed."})
		return "", err
	}

	if err := a.tokenStorage.SetAuthToken(token); err != nil {
		a.sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not store token in keyring: %v", err)})
	}

	a.sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: "Login successful."})
	return token, nil
}

// Logout removes the stored auth token from the keyring
func (a *Auth) Logout() error {
	a.sink.Emit(output.SpinnerStart("Logging out..."))

	_, err := a.tokenStorage.GetAuthToken()
	if err != nil {
		a.sink.Emit(output.SpinnerStop())
		if a.authToken != "" {
			a.sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Authenticated via LOCALSTACK_AUTH_TOKEN environment variable; unset it to log out"})
			return nil
		}
		if !errors.Is(err, ErrTokenNotFound) {
			return fmt.Errorf("failed to read auth token: %w", err)
		}
		a.sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Not currently logged in"})
		return ErrNotLoggedIn
	}

	if err := a.tokenStorage.DeleteAuthToken(); err != nil {
		a.sink.Emit(output.SpinnerStop())
		return fmt.Errorf("failed to delete auth token: %w", err)
	}

	if a.licenseFilePath != "" {
		_ = os.Remove(a.licenseFilePath)
	}

	a.sink.Emit(output.SpinnerStop())
	a.sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: "Logged out successfully"})
	return nil
}
