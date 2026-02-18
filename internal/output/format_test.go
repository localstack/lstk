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
			name:   "log event",
			event:  LogEvent{Message: "hello"},
			want:   "hello",
			wantOK: true,
		},
		{
			name:   "warning event",
			event:  WarningEvent{Message: "careful"},
			want:   "Warning: careful",
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
