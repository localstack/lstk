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
		event  Event
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
			want:   SuccessMarker() + " done",
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
			event:  AuthEvent{Preamble: "Welcome", Code: "ABCD-1234", URL: "https://example.com"},
			want:   "Welcome\nOpening browser to login...\nBrowser didn't open? Visit https://example.com\n\nOne-time code: ABCD-1234",
			wantOK: true,
		},
		{
			name:   "instructions event url only",
			event:  AuthEvent{URL: "https://example.com"},
			want:   "Opening browser to login...\nBrowser didn't open? Visit https://example.com",
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
			want:   SuccessMarker() + " LocalStack ready (abc123)",
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
			want:   "Error: Docker not running\n  Cannot connect to Docker daemon\n  ==> Start Docker: open -a Docker",
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
			want:   SuccessMarker() + " LocalStack AWS Emulator is running (localhost.localstack.cloud:4566)\n  UPTIME: 4m 23s · CONTAINER: localstack-aws · VERSION: 4.14.1",
			wantOK: true,
		},
		{
			name: "instance info minimal",
			event: InstanceInfoEvent{
				EmulatorName: "LocalStack AWS Emulator",
				Host:         "127.0.0.1:4566",
			},
			want:   SuccessMarker() + " LocalStack AWS Emulator is running (127.0.0.1:4566)",
			wantOK: true,
		},
		{
			name: "table with entries",
			event: TableEvent{
				Headers: []string{"Service", "Resource", "Region", "Account"},
				Rows: [][]string{
					{"Lambda", "handler", "us-east-1", "000000000000"},
					{"S3", "my-bucket", "us-east-1", "000000000000"},
				},
			},
			want:   "  SERVICE  RESOURCE   REGION     ACCOUNT\n  Lambda   handler    us-east-1  000000000000\n  S3       my-bucket  us-east-1  000000000000",
			wantOK: true,
		},
		{
			name:   "table empty",
			event:  TableEvent{Headers: []string{"A"}, Rows: [][]string{}},
			want:   "",
			wantOK: false,
		},
		{
			name:   "log line event info",
			event:  LogLineEvent{Source: LogSourceEmulator, Line: "INFO --- [] localstack.core : started", Level: LogLevelInfo},
			want:   "INFO --- [] localstack.core : started",
			wantOK: true,
		},
		{
			name:   "log line event unknown level",
			event:  LogLineEvent{Source: LogSourceEmulator, Line: "Docker not available", Level: LogLevelUnknown},
			want:   "Docker not available",
			wantOK: true,
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

func TestFormatTableWidth(t *testing.T) {
	t.Parallel()

	e := TableEvent{
		Headers: []string{"Service", "Resource", "Region", "Account"},
		Rows: [][]string{
			{"CloudFormation", "8245db0d-5c05-4209-90f0-51ec48446a58", "us-east-1", "000000000000"},
			{"EC2", "subnet-816649cee2efc65ac", "eu-central-1", "000000000000"},
			{"Lambda", "HelloWorldFunctionJavaScript", "us-east-1", "000000000000"},
		},
	}

	t.Run("truncates widest column to fit terminal width", func(t *testing.T) {
		t.Parallel()
		got := formatTableWidth(e, 80)
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
		got := formatTableWidth(e, 200)
		if strings.Contains(got, "…") {
			t.Error("expected no truncation at width 200")
		}
		if !strings.Contains(got, "8245db0d-5c05-4209-90f0-51ec48446a58") {
			t.Error("expected full UUID")
		}
	})

	t.Run("narrow terminal still renders without panic", func(t *testing.T) {
		t.Parallel()
		got := formatTableWidth(e, 40)
		if got == "" {
			t.Error("expected non-empty output")
		}
		if !strings.Contains(got, "…") {
			t.Error("expected truncation at narrow width")
		}
	})
}
