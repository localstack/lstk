package auth

//go:generate mockgen -source=login.go -destination=mock_login_test.go -package=auth

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/pkg/browser"
)

type LoginProvider interface {
	Login(ctx context.Context) (string, error)
}

type loginProvider struct {
	platformClient api.PlatformAPI
	sink           output.Sink
}

func newLoginProvider(sink output.Sink, platformClient api.PlatformAPI) *loginProvider {
	return &loginProvider{
		platformClient: platformClient,
		sink:           sink,
	}
}

func (l *loginProvider) Login(ctx context.Context) (string, error) {
	authReq, err := l.platformClient.CreateAuthRequest(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create auth request: %w", err)
	}

	authURL := fmt.Sprintf("%s/auth/request/%s", getWebAppURL(), authReq.ID)
	output.EmitLog(l.sink, fmt.Sprintf("Visit: %s", authURL))
	output.EmitLog(l.sink, fmt.Sprintf("Verification code: %s", authReq.Code))

	// Ask whether to open the browser; ENTER or Y accepts (default yes), N skips
	browserCh := make(chan output.InputResponse, 1)
	output.EmitUserInputRequest(l.sink, output.UserInputRequestEvent{
		Prompt:     "Open browser now?",
		Options:    []output.InputOption{{Key: "y", Label: "Y"}, {Key: "n", Label: "n"}},
		ResponseCh: browserCh,
	})

	select {
	case resp := <-browserCh:
		if resp.Cancelled {
			return "", context.Canceled
		}
		if resp.SelectedKey != "n" {
			if err := browser.OpenURL(authURL); err != nil {
				output.EmitLog(l.sink, fmt.Sprintf("Warning: Failed to open browser: %v", err))
			}
		}
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Wait for the user to complete authentication in the browser
	enterCh := make(chan output.InputResponse, 1)
	output.EmitUserInputRequest(l.sink, output.UserInputRequestEvent{
		Prompt:     "Waiting for authentication",
		Options:    []output.InputOption{{Key: "enter", Label: "Press ENTER when complete"}},
		ResponseCh: enterCh,
	})

	select {
	case resp := <-enterCh:
		if resp.Cancelled {
			return "", context.Canceled
		}
		return l.completeAuth(ctx, authReq)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func getWebAppURL() string {
	return env.Vars.WebAppURL
}

func (l *loginProvider) completeAuth(ctx context.Context, authReq *api.AuthRequest) (string, error) {
	output.EmitLog(l.sink, "Checking if auth request is confirmed...")
	confirmed, err := l.platformClient.CheckAuthRequestConfirmed(ctx, authReq.ID, authReq.ExchangeToken)
	if err != nil {
		return "", fmt.Errorf("failed to check auth request: %w", err)
	}
	if !confirmed {
		return "", fmt.Errorf("auth request not confirmed - please complete the authentication in your browser")
	}
	output.EmitLog(l.sink, "Auth request confirmed, exchanging for token...")

	bearerToken, err := l.platformClient.ExchangeAuthRequest(ctx, authReq.ID, authReq.ExchangeToken)
	if err != nil {
		return "", fmt.Errorf("failed to exchange auth request: %w", err)
	}

	output.EmitLog(l.sink, "Fetching license token...")
	licenseToken, err := l.platformClient.GetLicenseToken(ctx, bearerToken)
	if err != nil {
		return "", fmt.Errorf("failed to get license token: %w", err)
	}

	return licenseToken, nil
}
