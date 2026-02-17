package auth

//go:generate mockgen -source=login.go -destination=mock_login_test.go -package=auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
	"github.com/pkg/browser"
)

const webAppURL = "https://app.localstack.cloud"
const loginCallbackURL = "127.0.0.1:45678"

type LoginProvider interface {
	Login(ctx context.Context) (string, error)
}

type browserLogin struct {
	platformClient api.PlatformAPI
	sink           output.Sink
}

func newBrowserLogin(sink output.Sink, platformClient api.PlatformAPI) *browserLogin {
	return &browserLogin{
		platformClient: platformClient,
		sink:           sink,
	}
}

func startCallbackServer() (*http.Server, chan string, chan error, error) {
	listener, err := net.Listen("tcp", loginCallbackURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to start callback server: %w", err)
	}

	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/success", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			errCh <- fmt.Errorf("no token in callback")
			http.Error(w, "No token received", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		tokenCh <- token
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	return server, tokenCh, errCh, nil
}

func (b *browserLogin) Login(ctx context.Context) (string, error) {
	server, tokenCh, errCh, err := startCallbackServer()
	if err != nil {
		return "", err
	}
	defer func() {
		if err := server.Shutdown(ctx); err != nil {
			output.EmitWarning(b.sink, fmt.Sprintf("failed to shutdown server: %v", err))
		}
	}()

	// Device flow as fallback
	authReq, err := b.platformClient.CreateAuthRequest(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create auth request: %w", err)
	}

	deviceURL := fmt.Sprintf("%s/auth/request/%s", getWebAppURL(), authReq.ID)

	// Try to open browser
	loginURL := fmt.Sprintf("%s/redirect?name=CLI", getWebAppURL())
	browserOpened := browser.OpenURL(loginURL) == nil

	// Display device flow instructions
	if browserOpened {
		output.EmitLog(b.sink, fmt.Sprintf("Browser didn't open? Open %s to authorize device.", deviceURL))
	} else {
		output.EmitLog(b.sink, fmt.Sprintf("Open %s to authorize device.", deviceURL))
	}
	output.EmitLog(b.sink, fmt.Sprintf("Verification code: %s", authReq.Code))

	// Emit user input request event
	responseCh := make(chan output.InputResponse, 1)
	output.EmitUserInputRequest(b.sink, output.UserInputRequestEvent{
		Prompt:     "Waiting for authentication...",
		Options:    []output.InputOption{{Key: "enter", Label: "Press ENTER when complete"}},
		ResponseCh: responseCh,
	})

	// In test mode, auto-respond to the input request
	if os.Getenv("LSTK_TEST_AUTO_ENTER") == "1" {
		go func() {
			responseCh <- output.InputResponse{SelectedKey: "enter"}
		}()
	}

	// Wait for either browser callback, user response, or context cancellation
	select {
	case token := <-tokenCh:
		return token, nil
	case err := <-errCh:
		return "", err
	case resp := <-responseCh:
		if resp.Cancelled {
			return "", context.Canceled
		}
		// User pressed ENTER, try device flow
		return b.completeDeviceFlow(ctx, authReq)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func getWebAppURL() string {
	// allows overriding the URL for testing
	if url := os.Getenv("LOCALSTACK_WEB_APP_URL"); url != "" {
		return url
	}
	return webAppURL
}

func (b *browserLogin) completeDeviceFlow(ctx context.Context, authReq *api.AuthRequest) (string, error) {
	output.EmitLog(b.sink, "Checking if auth request is confirmed...")
	confirmed, err := b.platformClient.CheckAuthRequestConfirmed(ctx, authReq.ID, authReq.ExchangeToken)
	if err != nil {
		return "", fmt.Errorf("failed to check auth request: %w", err)
	}
	if !confirmed {
		return "", fmt.Errorf("auth request not confirmed - please enter the code in the browser first")
	}
	output.EmitLog(b.sink, "Auth request confirmed, exchanging for token...")

	bearerToken, err := b.platformClient.ExchangeAuthRequest(ctx, authReq.ID, authReq.ExchangeToken)
	if err != nil {
		return "", fmt.Errorf("failed to exchange auth request: %w", err)
	}

	output.EmitLog(b.sink, "Fetching license token...")
	licenseToken, err := b.platformClient.GetLicenseToken(ctx, bearerToken)
	if err != nil {
		return "", fmt.Errorf("failed to get license token: %w", err)
	}

	return licenseToken, nil
}
