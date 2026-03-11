package output

import (
	"strings"
	"testing"
	"time"
)

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
			name:   "status pulling suppressed",
			event:  ContainerStatusEvent{Phase: "pulling", Container: "localstack/localstack:latest"},
			want:   "",
			wantOK: false,
		},
		{
			name:   "status ready with detail",
			event:  ContainerStatusEvent{Phase: "ready", Container: "localstack", Detail: "abc123"},
			want:   "localstack ready (abc123)",
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
			name: "instance info full",
			event: InstanceInfoEvent{
				EmulatorName:  "LocalStack AWS Emulator",
				Version:       "4.14.1",
				Host:          "localhost.localstack.cloud:4566",
				ContainerName: "localstack-aws",
				Uptime:        4*time.Minute + 23*time.Second,
			},
			want:   "✓ LocalStack AWS Emulator is running (localhost.localstack.cloud:4566)\n  UPTIME: 4m 23s · CONTAINER: localstack-aws · VERSION: 4.14.1",
			wantOK: true,
		},
		{
			name: "instance info minimal",
			event: InstanceInfoEvent{
				EmulatorName: "LocalStack AWS Emulator",
				Host:         "127.0.0.1:4566",
			},
			want:   "✓ LocalStack AWS Emulator is running (127.0.0.1:4566)",
			wantOK: true,
		},
		{
			name: "resource summary",
			event: ResourceSummaryEvent{
				ResourceCount: 23,
				ServiceCount:  12,
			},
			want:   "~ 23 resources · 12 services",
			wantOK: true,
		},
		{
			name: "resource table with entries",
			event: ResourceTableEvent{
				Rows: []ResourceRow{
					{Service: "Lambda", Resource: "handler", Region: "us-east-1", Account: "000000000000"},
					{Service: "S3", Resource: "my-bucket", Region: "us-east-1", Account: "000000000000"},
				},
			},
			want:   "  SERVICE  RESOURCE   REGION     ACCOUNT\n  Lambda   handler    us-east-1  000000000000\n  S3       my-bucket  us-east-1  000000000000",
			wantOK: true,
		},
		{
			name:   "resource table empty",
			event:  ResourceTableEvent{Rows: []ResourceRow{}},
			want:   "",
			wantOK: false,
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

func TestFormatResourceTableWidth(t *testing.T) {
	t.Parallel()

	e := ResourceTableEvent{
		Rows: []ResourceRow{
			{Service: "CloudFormation", Resource: "8245db0d-5c05-4209-90f0-51ec48446a58", Region: "us-east-1", Account: "000000000000"},
			{Service: "EC2", Resource: "subnet-816649cee2efc65ac", Region: "eu-central-1", Account: "000000000000"},
			{Service: "Lambda", Resource: "HelloWorldFunctionJavaScript", Region: "us-east-1", Account: "000000000000"},
		},
	}

	t.Run("truncates resource column to fit terminal width", func(t *testing.T) {
		t.Parallel()
		got := formatResourceTableWidth(e, 80)
		for i, line := range strings.Split(got, "\n") {
			w := displayWidth(line)
			if w > 80 {
				t.Errorf("line %d has display width %d (>80): %q", i, w, line)
			}
		}
		if !strings.Contains(got, "8245db0d") {
			t.Error("expected truncated UUID to still contain prefix")
		}
		if !strings.Contains(got, "…") {
			t.Error("expected truncation marker")
		}
	})

	t.Run("no truncation when terminal is wide enough", func(t *testing.T) {
		t.Parallel()
		got := formatResourceTableWidth(e, 200)
		if strings.Contains(got, "…") {
			t.Error("expected no truncation at width 200")
		}
		if !strings.Contains(got, "8245db0d-5c05-4209-90f0-51ec48446a58") {
			t.Error("expected full UUID")
		}
	})

	t.Run("narrow terminal still renders without panic", func(t *testing.T) {
		t.Parallel()
		got := formatResourceTableWidth(e, 40)
		if got == "" {
			t.Error("expected non-empty output")
		}
		if !strings.Contains(got, "…") {
			t.Error("expected truncation at narrow width")
		}
	})
}

