package auth

//go:generate mockgen -source=login.go -destination=mock_login_test.go -package=auth

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
	"github.com/pkg/browser"
)

type LoginProvider interface {
	Login(ctx context.Context) (string, error)
}

type loginProvider struct {
	platformClient api.PlatformAPI
	sink           output.Sink
	webAppURL      string
}

func newLoginProvider(sink output.Sink, platformClient api.PlatformAPI, webAppURL string) *loginProvider {
	return &loginProvider{
		platformClient: platformClient,
		sink:           sink,
		webAppURL:      webAppURL,
	}
}

func (l *loginProvider) Login(ctx context.Context) (string, error) {
	authReq, err := l.platformClient.CreateAuthRequest(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create auth request: %w", err)
	}

	authURL := buildAuthURL(l.webAppURL, authReq.ID, authReq.Code)

	l.sink.Emit(output.AuthEvent{
		Preamble: "Welcome to lstk, a command-line interface for LocalStack",
		Code:     authReq.Code,
		URL:      authURL,
	})
	browser.Stdout = io.Discard
	browser.Stderr = io.Discard
	if err := browser.OpenURL(authURL); err != nil {
		l.sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Failed to open browser automatically. Open this URL manually to continue: %s", authURL)})
	}

	l.sink.Emit(output.SpinnerStart("Waiting for authorization..."))

	responseCh := make(chan output.InputResponse, 1)
	l.sink.Emit(output.UserInputRequestEvent{
		Prompt:     "Waiting for authorization...",
		Options:    []output.InputOption{{Key: "any", Label: "Press any key when complete"}},
		ResponseCh: responseCh,
	})

	select {
	case resp := <-responseCh:
		l.sink.Emit(output.SpinnerStop())
		if resp.Cancelled {
			return "", context.Canceled
		}
		return l.completeAuth(ctx, authReq)
	case <-ctx.Done():
		l.sink.Emit(output.SpinnerStop())
		return "", ctx.Err()
	}
}

func buildAuthURL(webAppURL, authRequestID, code string) string {
	authURL := fmt.Sprintf("%s/auth/request/%s", strings.TrimRight(webAppURL, "/"), authRequestID)
	if code == "" {
		return authURL
	}

	values := url.Values{}
	values.Set("code", code)
	return authURL + "?" + values.Encode()
}

func (l *loginProvider) completeAuth(ctx context.Context, authReq *api.AuthRequest) (string, error) {
	l.sink.Emit(output.MessageEvent{Severity: output.SeverityInfo, Text: "Checking if auth request is confirmed..."})
	confirmed, err := l.platformClient.CheckAuthRequestConfirmed(ctx, authReq.ID, authReq.ExchangeToken)
	if err != nil {
		return "", fmt.Errorf("failed to check auth request: %w", err)
	}
	if !confirmed {
		return "", fmt.Errorf("auth request not confirmed - please complete the authentication in your browser")
	}
	l.sink.Emit(output.MessageEvent{Severity: output.SeverityInfo, Text: "Auth request confirmed, exchanging for token..."})

	bearerToken, err := l.platformClient.ExchangeAuthRequest(ctx, authReq.ID, authReq.ExchangeToken)
	if err != nil {
		return "", fmt.Errorf("failed to exchange auth request: %w", err)
	}

	l.sink.Emit(output.MessageEvent{Severity: output.SeverityInfo, Text: "Fetching license token..."})
	licenseToken, err := l.platformClient.GetLicenseToken(ctx, bearerToken)
	if err != nil {
		return "", fmt.Errorf("failed to get license token: %w", err)
	}

	return licenseToken, nil
}
