package auth

import (
	"strings"
	"testing"
)

func TestBuildAuthURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		webAppURL   string
		requestID   string
		code        string
		wantAuthURL string
	}{
		{
			name:        "adds code query param",
			webAppURL:   "http://app.localstack.cloud",
			requestID:   "d78cc482-1db6-4d4d-9f9c-3512963fdf93",
			code:        "1234",
			wantAuthURL: "http://app.localstack.cloud/auth/request/d78cc482-1db6-4d4d-9f9c-3512963fdf93?code=1234",
		},
		{
			name:        "escapes query param",
			webAppURL:   "https://example.com",
			requestID:   "req-id",
			code:        "A B+C",
			wantAuthURL: "https://example.com/auth/request/req-id?code=A+B%2BC",
		},
		{
			name:        "omits empty code",
			webAppURL:   "https://example.com",
			requestID:   "req-id",
			code:        "",
			wantAuthURL: "https://example.com/auth/request/req-id",
		},
		{
			name:        "trims trailing slash from web app URL",
			webAppURL:   "https://example.com/",
			requestID:   "req-id",
			code:        "1234",
			wantAuthURL: "https://example.com/auth/request/req-id?code=1234",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := buildAuthURL(tt.webAppURL, tt.requestID, tt.code)
			if got != tt.wantAuthURL {
				t.Fatalf("expected auth URL %q, got %q", tt.wantAuthURL, got)
			}
			if strings.Contains(got, "//auth/request") {
				t.Fatalf("expected auth URL without double slash before auth/request, got %q", got)
			}
		})
	}
}
