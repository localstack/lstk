package container

import (
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
)

func TestParseLogLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		line        string
		wantLevel   output.LogLevel
		wantLogger  string
	}{
		{
			name:       "INFO aws request",
			line:       "2026-03-16T17:56:43.472  INFO --- [et.reactor-0] localstack.request.aws     : AWS iam.GetUser => 200",
			wantLevel:  output.LogLevelInfo,
			wantLogger: "localstack.request.aws",
		},
		{
			name:       "INFO http request",
			line:       "2026-03-16T17:58:36.596  INFO --- [et.reactor-1] localstack.request.http    : GET /_localstack/resources => 200",
			wantLevel:  output.LogLevelInfo,
			wantLogger: "localstack.request.http",
		},
		{
			name:       "WARN internal handler",
			line:       "2026-03-16T18:10:35.149  WARN --- [et.reactor-1] l.aws.handlers.internal    : Unable to find resource handler for path: /_localstack/appinspector/v1/traces/*/spans",
			wantLevel:  output.LogLevelWarn,
			wantLogger: "l.aws.handlers.internal",
		},
		{
			name:       "INFO provider log",
			line:       "2026-03-16T17:56:51.985  INFO --- [et.reactor-0] l.p.c.services.s3.provider : Using /tmp/localstack/state/s3 as storage path for s3 assets",
			wantLevel:  output.LogLevelInfo,
			wantLogger: "l.p.c.services.s3.provider",
		},
		{
			name:       "INFO extensions plugin",
			line:       "2026-03-16T17:56:00.810  INFO --- [  MainThread] l.p.c.extensions.plugins   : loaded 0 extensions",
			wantLevel:  output.LogLevelInfo,
			wantLogger: "l.p.c.extensions.plugins",
		},
		{
			name:       "ERROR level",
			line:       "2026-03-16T17:56:00.810 ERROR --- [  MainThread] localstack.core            : something failed",
			wantLevel:  output.LogLevelError,
			wantLogger: "localstack.core",
		},
		{
			name:       "non-standard line",
			line:       "Docker not available",
			wantLevel:  output.LogLevelUnknown,
			wantLogger: "",
		},
		{
			name:       "empty line",
			line:       "",
			wantLevel:  output.LogLevelUnknown,
			wantLogger: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			level, logger := parseLogLine(tt.line)
			assert.Equal(t, tt.wantLevel, level)
			assert.Equal(t, tt.wantLogger, logger)
		})
	}
}

func TestShouldFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		line   string
		filter bool
	}{
		{
			name:   "HTTP request log",
			line:   "2026-03-16T17:58:36.596  INFO --- [et.reactor-1] localstack.request.http    : GET /_localstack/resources => 200",
			filter: true,
		},
		{
			name:   "HTTP OPTIONS request",
			line:   "2026-03-16T18:10:35.142  INFO --- [et.reactor-4] localstack.request.http    : OPTIONS /_localstack/appinspector/status => 204",
			filter: true,
		},
		{
			name:   "internal handler warning",
			line:   "2026-03-16T18:10:35.149  WARN --- [et.reactor-1] l.aws.handlers.internal    : Unable to find resource handler for path: /_localstack/appinspector/v1/traces/*/spans",
			filter: true,
		},
		{
			name:   "provider debug log",
			line:   "2026-03-16T17:56:51.985  INFO --- [et.reactor-0] l.p.c.services.s3.provider : Using /tmp/localstack/state/s3 as storage path for s3 assets",
			filter: true,
		},
		{
			name:   "Docker not available",
			line:   "Docker not available",
			filter: true,
		},
		{
			name:   "AWS request log - kept",
			line:   "2026-03-16T17:56:43.472  INFO --- [et.reactor-0] localstack.request.aws     : AWS iam.GetUser => 200",
			filter: false,
		},
		{
			name:   "extensions plugin log - kept",
			line:   "2026-03-16T17:56:00.810  INFO --- [  MainThread] l.p.c.extensions.plugins   : loaded 0 extensions",
			filter: false,
		},
		{
			name:   "IAM plugin log - kept",
			line:   "2026-03-16T17:56:43.344  INFO --- [et.reactor-0] l.p.c.services.iam.plugins : Configured IAM provider to use Advance Policy Simulator",
			filter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.filter, shouldFilter(tt.line))
		})
	}
}
