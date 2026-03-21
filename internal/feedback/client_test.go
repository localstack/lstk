package feedback

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/localstack/lstk/internal/version"
)

func TestSubmitPostsFeedbackToPlatformAPI(t *testing.T) {
	t.Parallel()

	var authHeader string
	var payload submitRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/feedback" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	err := client.Submit(context.Background(), SubmitInput{
		Message:   "Something feels off when starting LocalStack",
		AuthToken: "auth-token",
		Context: Context{
			AuthConfigured:   true,
			InstallMethod:    "homebrew",
			Shell:            "zsh",
			ContainerRuntime: "orbstack",
			ConfigPath:       "/tmp/config.toml",
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if authHeader != "Basic OmF1dGgtdG9rZW4=" {
		t.Fatalf("expected basic auth token, got %q", authHeader)
	}
	if payload.Message != "Something feels off when starting LocalStack" {
		t.Fatalf("unexpected message %q", payload.Message)
	}
	assertEqual(t, payload.Metadata["version (lstk)"], version.Version())
	assertEqual(t, payload.Metadata["os (arch)"], runtime.GOOS+" ("+runtime.GOARCH+")")
	assertEqual(t, payload.Metadata["installation"], "homebrew")
	assertEqual(t, payload.Metadata["shell"], "zsh")
	assertEqual(t, payload.Metadata["container runtime"], "orbstack")
	assertEqual(t, payload.Metadata["auth"], "Configured")
	assertEqual(t, payload.Metadata["config"], "/tmp/config.toml")
}

func TestSubmitReturnsPlatformError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":true,"message":"generic.bad_request"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	err := client.Submit(context.Background(), SubmitInput{
		Message:   "hello",
		AuthToken: "auth-token",
	})
	if err == nil || err.Error() != `feedback API returned 400 Bad Request: {"error":true,"message":"generic.bad_request"}` {
		t.Fatalf("expected platform error, got %v", err)
	}
}

func assertEqual(t *testing.T, got, want any) {
	t.Helper()
	if got != want {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
