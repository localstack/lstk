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
			name:   "status pulling",
			event:  ContainerStatusEvent{Phase: "pulling", Container: "localstack/localstack:latest"},
			want:   "Pulling localstack/localstack:latest...",
			wantOK: true,
		},
		{
			name:   "status ready with detail",
			event:  ContainerStatusEvent{Phase: "ready", Container: "localstack", Detail: "abc123"},
			want:   "localstack ready (abc123)",
			wantOK: true,
		},
		{
			name:   "progress with total",
			event:  ProgressEvent{LayerID: "abc123", Status: "Downloading", Current: 50, Total: 100},
			want:   "  abc123: Downloading 50.0%",
			wantOK: true,
		},
		{
			name:   "progress with status only",
			event:  ProgressEvent{LayerID: "abc123", Status: "Pull complete"},
			want:   "  abc123: Pull complete",
			wantOK: true,
		},
		{
			name:   "progress ignored when empty",
			event:  ProgressEvent{LayerID: "abc123"},
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
