package output

import (
	"fmt"
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
			want:   SuccessMarker() + " LocalStack is running (abc123)",
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
				Persistence:   true,
			},
			want:   SuccessMarker() + " LocalStack AWS Emulator is running\n• Endpoint: localhost.localstack.cloud:4566\n• Persistence: Enabled\n• Container: localstack-aws\n• Version: 4.14.1\n• Uptime: 4m 23s",
			wantOK: true,
		},
		{
			name: "instance info minimal",
			event: InstanceInfoEvent{
				EmulatorName: "LocalStack AWS Emulator",
				Host:         "127.0.0.1:4566",
			},
			want:   SuccessMarker() + " LocalStack AWS Emulator is running\n• Endpoint: 127.0.0.1:4566",
			wantOK: true,
		},
		{
			name: "instance info omits persistence when disabled",
			event: InstanceInfoEvent{
				EmulatorName:  "LocalStack AWS Emulator",
				Host:          "127.0.0.1:4566",
				ContainerName: "localstack-aws",
				Persistence:   false,
			},
			want:   SuccessMarker() + " LocalStack AWS Emulator is running\n• Endpoint: 127.0.0.1:4566\n• Container: localstack-aws",
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
		{
			name:   "auth complete event",
			event:  AuthCompleteEvent{},
			want:   "",
			wantOK: false,
		},
		{
			name:   "emulator stopped event",
			event:  EmulatorStoppedEvent{Type: "aws", Name: "localstack-aws", DisplayName: "LocalStack AWS Emulator", WasRunning: true},
			want:   SuccessMarker() + " LocalStack AWS Emulator stopped",
			wantOK: true,
		},
		{
			name:   "emulator reset event",
			event:  EmulatorResetEvent{Type: "aws", Name: "localstack-aws"},
			want:   SuccessMarker() + " Emulator state reset",
			wantOK: true,
		},
		{
			name:   "update checked event dev build",
			event:  UpdateCheckedEvent{CurrentVersion: "dev", DevBuild: true},
			want:   "> Note: Running a development build, skipping update check",
			wantOK: true,
		},
		{
			name:   "update checked event up to date",
			event:  UpdateCheckedEvent{CurrentVersion: "2.3.0", LatestVersion: "2.3.0", Available: false},
			want:   "> Note: Already up to date (2.3.0)",
			wantOK: true,
		},
		{
			name:   "update checked event available",
			event:  UpdateCheckedEvent{CurrentVersion: "2.2.1", LatestVersion: "2.3.0", Available: true},
			want:   "Update available: 2.2.1 → 2.3.0",
			wantOK: true,
		},
		{
			name:   "update applied event",
			event:  UpdateAppliedEvent{CurrentVersion: "2.2.1", UpdatedVersion: "2.3.0", Method: "homebrew"},
			want:   SuccessMarker() + " Updated to 2.3.0",
			wantOK: true,
		},
		{
			name: "multiple installs event",
			event: MultipleInstallsEvent{Installs: []InstallLocation{
				{Path: "/opt/homebrew/bin/lstk", Method: "homebrew", Running: true},
				{Path: "/home/u/.nvm/versions/node/v22/bin/lstk", Method: "npm"},
			}},
			want: "> Warning: Multiple lstk installations found on PATH:\n" +
				"  /opt/homebrew/bin/lstk (homebrew, currently running)\n" +
				"  /home/u/.nvm/versions/node/v22/bin/lstk (npm)\n" +
				"  Your shell runs the first one; remove the others to avoid using a stale version.",
			wantOK: true,
		},
		{
			name: "multiple installs event no running marker",
			event: MultipleInstallsEvent{Installs: []InstallLocation{
				{Path: "/usr/local/bin/lstk", Method: "binary"},
				{Path: "/opt/homebrew/bin/lstk", Method: "homebrew"},
			}},
			want: "> Warning: Multiple lstk installations found on PATH:\n" +
				"  /usr/local/bin/lstk (binary)\n" +
				"  /opt/homebrew/bin/lstk (homebrew)\n" +
				"  Your shell runs the first one; remove the others to avoid using a stale version.",
			wantOK: true,
		},
		{
			name: "pod snapshot saved full",
			event: PodSnapshotSavedEvent{
				PodName:  "my-baseline",
				Version:  3,
				Services: []string{"dynamodb", "s3", "sqs"},
				Size:     2621440,
			},
			want:   SuccessMarker() + " Snapshot saved to pod:my-baseline\n• Version: 3\n• Services: dynamodb, s3, sqs\n• Size: 2.5 MB",
			wantOK: true,
		},
		{
			name: "pod snapshot saved many services",
			event: PodSnapshotSavedEvent{
				PodName:  "big-pod",
				Version:  1,
				Services: []string{"s3", "sqs", "sns", "dynamodb", "lambda", "apigateway", "iam", "sts", "ec2", "rds", "kinesis", "firehose", "cloudwatch", "cloudformation", "route53"},
				Size:     10485760,
			},
			want:   SuccessMarker() + " Snapshot saved to pod:big-pod\n• Version: 1\n• Services: s3, sqs, sns, dynamodb, lambda, apigateway, iam, sts, ec2, rds, kinesis, firehose, cloudwatch, cloudformation, route53\n• Size: 10.0 MB",
			wantOK: true,
		},
		{
			name: "pod snapshot saved omits zero fields",
			event: PodSnapshotSavedEvent{
				PodName: "minimal-pod",
			},
			want:   SuccessMarker() + " Snapshot saved to pod:minimal-pod",
			wantOK: true,
		},
		{
			name: "remote snapshot saved full",
			event: RemoteSnapshotSavedEvent{
				PodName:  "my-baseline",
				Location: "s3://my-bucket/prefix",
				Version:  3,
				Services: []string{"dynamodb", "s3", "sqs"},
				Size:     2621440,
			},
			want:   SuccessMarker() + " Snapshot saved to s3://my-bucket/prefix as \"my-baseline\"\n• Version: 3\n• Services: dynamodb, s3, sqs\n• Size: 2.5 MB",
			wantOK: true,
		},
		{
			name: "remote snapshot saved omits zero fields",
			event: RemoteSnapshotSavedEvent{
				PodName:  "minimal-pod",
				Location: "s3://my-bucket",
			},
			want:   SuccessMarker() + " Snapshot saved to s3://my-bucket as \"minimal-pod\"",
			wantOK: true,
		},

		// snapshot load events
		{
			name:   "snapshot loaded with services",
			event:  SnapshotLoadedEvent{Source: "./my-baseline.snapshot", Services: []string{"s3", "dynamodb"}},
			want:   SuccessMarker() + " Snapshot loaded from ./my-baseline.snapshot\n• Services: s3, dynamodb",
			wantOK: true,
		},
		{
			name:   "snapshot loaded no services",
			event:  SnapshotLoadedEvent{Source: "./snap.snapshot"},
			want:   SuccessMarker() + " Snapshot loaded from ./snap.snapshot",
			wantOK: true,
		},
		{
			name:   "pod snapshot loaded with services",
			event:  SnapshotLoadedEvent{Source: "pod:my-baseline", Services: []string{"s3", "lambda"}},
			want:   SuccessMarker() + " Snapshot loaded from pod:my-baseline\n• Services: s3, lambda",
			wantOK: true,
		},
		{
			name:   "pod snapshot loaded no services",
			event:  SnapshotLoadedEvent{Source: "pod:empty-pod"},
			want:   SuccessMarker() + " Snapshot loaded from pod:empty-pod",
			wantOK: true,
		},

		// deferred events — plain sinks render the inner event immediately
		{
			name:   "deferred note message",
			event:  DeferredEvent{Inner: MessageEvent{Severity: SeverityNote, Text: "No snapshots found"}},
			want:   "> Note: No snapshots found",
			wantOK: true,
		},
		{
			name:   "deferred secondary message",
			event:  DeferredEvent{Inner: MessageEvent{Severity: SeveritySecondary, Text: "~ 2 snapshots"}},
			want:   "~ 2 snapshots",
			wantOK: true,
		},
		{
			name: "deferred table",
			event: DeferredEvent{Inner: TableEvent{
				Headers: []string{"Name", "Version", "Last Changed"},
				Rows: [][]string{
					{"baseline-q2", "3", "2026-04-15 14:32 UTC"},
					{"infra-2026-04", "1", "-"},
				},
			}},
			want:   "  NAME           VERSION  LAST CHANGED\n  baseline-q2    3        2026-04-15 14:32 UTC\n  infra-2026-04  1        -",
			wantOK: true,
		},

		// snapshot remove events
		{
			name:   "pod snapshot removed",
			event:  PodSnapshotRemovedEvent{PodName: "my-baseline"},
			want:   SuccessMarker() + " Cloud snapshot 'pod:my-baseline' deleted",
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

func TestFormatSnapshotShown(t *testing.T) {
	t.Parallel()

	label := func(s string) string { return fmt.Sprintf("%-*s", snapshotShowLabelWidth, s) }
	resLabel := func(s string) string { return "  " + fmt.Sprintf("%-*s", snapshotShowLabelWidth-2, s) }
	created := time.Date(2026, 4, 15, 14, 32, 0, 0, time.UTC)

	tests := []struct {
		name  string
		event SnapshotShownEvent
		want  string
	}{
		{
			name: "full detail with resources",
			event: SnapshotShownEvent{
				Name:              "my-baseline",
				Created:           &created,
				Size:              49597645,
				LocalStackVersion: "2026.03",
				Message:           "Pre-refactor baseline",
				Services:          []string{"s3", "lambda", "dynamodb", "sqs"},
				Resources: []SnapshotResourceLine{
					{Service: "s3", Counts: []SnapshotResourceCount{{Count: 3, Noun: "buckets"}}},
					{Service: "lambda", Counts: []SnapshotResourceCount{{Count: 12, Noun: "functions"}}},
					{Service: "dynamodb", Counts: []SnapshotResourceCount{{Count: 2, Noun: "tables"}}},
					{Service: "sqs", Counts: []SnapshotResourceCount{{Count: 5, Noun: "queues"}}},
				},
			},
			want: strings.Join([]string{
				label("Name") + "my-baseline",
				label("Created") + "2026-04-15 14:32 UTC",
				label("Size") + "47.3 MB",
				label("LocalStack") + "2026.03",
				label("Message") + "Pre-refactor baseline",
				"",
				label("Services") + "s3, lambda, dynamodb, sqs",
				"",
				"Resources",
				resLabel("s3") + "3 buckets",
				resLabel("lambda") + "12 functions",
				resLabel("dynamodb") + "2 tables",
				resLabel("sqs") + "5 queues",
			}, "\n"),
		},
		{
			name: "no resources renders services only",
			event: SnapshotShownEvent{
				Name:              "bare",
				LocalStackVersion: "2026.03",
				Services:          []string{"s3", "sqs"},
			},
			want: strings.Join([]string{
				label("Name") + "bare",
				label("LocalStack") + "2026.03",
				"",
				label("Services") + "s3, sqs",
			}, "\n"),
		},
		{
			name: "multiple resource kinds per service joined",
			event: SnapshotShownEvent{
				Name:     "multi",
				Services: []string{"lambda"},
				Resources: []SnapshotResourceLine{
					{Service: "lambda", Counts: []SnapshotResourceCount{{Count: 12, Noun: "functions"}, {Count: 3, Noun: "layers"}}},
				},
			},
			want: strings.Join([]string{
				label("Name") + "multi",
				"",
				label("Services") + "lambda",
				"",
				"Resources",
				resLabel("lambda") + "12 functions, 3 layers",
			}, "\n"),
		},
		{
			name:  "minimal omits empty fields and sections",
			event: SnapshotShownEvent{Name: "minimal"},
			want:  label("Name") + "minimal",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := FormatEventLine(tt.event)
			if !ok {
				t.Fatalf("expected ok=true")
			}
			if got != tt.want {
				t.Fatalf("expected:\n%q\ngot:\n%q", tt.want, got)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{2621440, "2.5 MB"},
		{1073741824, "1.0 GB"},
		{2684354560, "2.5 GB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Fatalf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
