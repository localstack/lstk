package output

import "testing"

func TestFormatEventLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		event  any
		want   string
		wantOK bool
	}{
		{
			name:   "message event info",
			event:  MessageEvent{Severity: SeverityInfo, Text: "hello"},
			want:   "hello",
			wantOK: true,
		},
		{
			name:   "message event success",
			event:  MessageEvent{Severity: SeveritySuccess, Text: "done"},
			want:   "> Success: done",
			wantOK: true,
		},
		{
			name:   "message event note",
			event:  MessageEvent{Severity: SeverityNote, Text: "fyi"},
			want:   "> Note: fyi",
			wantOK: true,
		},
		{
			name:   "message event warning",
			event:  MessageEvent{Severity: SeverityWarning, Text: "careful"},
			want:   "> Warning: careful",
			wantOK: true,
		},
		{
			name:   "instructions event full",
			event:  AuthEvent{Preamble: "Welcome", Code: "ABC123", URL: "https://example.com"},
			want:   "Welcome\nYour one-time code: ABC123\nOpening browser to login...\nhttps://example.com",
			wantOK: true,
		},
		{
			name:   "instructions event code only",
			event:  AuthEvent{Code: "XYZ"},
			want:   "Your one-time code: XYZ",
			wantOK: true,
		},
		{
			name:   "status pulling",
			event:  ContainerStatusEvent{Phase: "pulling", Container: "localstack/localstack:latest"},
			want:   "Preparing LocalStack...",
			wantOK: true,
		},
		{
			name:   "status ready with detail",
			event:  ContainerStatusEvent{Phase: "ready", Container: "localstack-aws", Detail: "abc123"},
			want:   "LocalStack ready (abc123)",
			wantOK: true,
		},
		{
			name:   "progress suppressed",
			event:  ProgressEvent{LayerID: "abc123", Status: "Downloading", Current: 50, Total: 100},
			want:   "",
			wantOK: false,
		},
		{
			name:   "spinner event active",
			event:  SpinnerEvent{Active: true, Text: "Loading"},
			want:   "Loading...",
			wantOK: true,
		},
		{
			name:   "spinner event stop",
			event:  SpinnerEvent{Active: false},
			want:   "",
			wantOK: false,
		},
		{
			name:   "error event title only",
			event:  ErrorEvent{Title: "Connection failed"},
			want:   "Error: Connection failed",
			wantOK: true,
		},
		{
			name:   "error event with summary",
			event:  ErrorEvent{Title: "Auth failed", Summary: "Invalid token"},
			want:   "Error: Auth failed\n  Invalid token",
			wantOK: true,
		},
		{
			name:   "error event with detail",
			event:  ErrorEvent{Title: "Auth failed", Summary: "Invalid token", Detail: "Token expired at 2024-01-01"},
			want:   "Error: Auth failed\n  Invalid token\n  Token expired at 2024-01-01",
			wantOK: true,
		},
		{
			name: "error event with actions",
			event: ErrorEvent{
				Title:   "Docker not running",
				Summary: "Cannot connect to Docker daemon",
				Actions: []ErrorAction{
					{Label: "Start Docker:", Value: "open -a Docker"},
				},
			},
			want:   "Error: Docker not running\n  Cannot connect to Docker daemon\n  → Start Docker: open -a Docker",
			wantOK: true,
		},
		{
			name:   "unsupported event",
			event:  struct{}{},
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := FormatEventLine(tt.event)
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got %v", tt.wantOK, ok)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
