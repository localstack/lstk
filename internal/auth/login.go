package auth

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/pkg/browser"
)

func getWebAppURL() string {
	// allows overriding the URL for testing
	if url := os.Getenv("LOCALSTACK_WEB_APP_URL"); url != "" {
		return url
	}
	return "https://app.localstack.cloud"
}

func Login(ctx context.Context) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:45678")
	if err != nil {
		return "", fmt.Errorf("failed to start callback server: %w", err)
	}
	defer listener.Close()

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
	go server.Serve(listener)
	defer server.Shutdown(ctx)

	loginURL := fmt.Sprintf("%s/redirect?name=CLI", getWebAppURL())
	if err := browser.OpenURL(loginURL); err != nil {
		log.Printf("Could not open browser. Please visit: %s", loginURL)
	}

	select {
	case token := <-tokenCh:
		return token, nil
	case err := <-errCh:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
