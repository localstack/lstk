package container

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// captureSink records every LogLineEvent it receives.
type captureSink struct {
	mu    sync.Mutex
	lines []output.LogLineEvent
}

func (s *captureSink) Emit(e output.Event) {
	if le, ok := e.(output.LogLineEvent); ok {
		s.mu.Lock()
		s.lines = append(s.lines, le)
		s.mu.Unlock()
	}
}

func runLogs(t *testing.T, content string, verbose bool) *captureSink {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	mockRT.EXPECT().
		StreamLogs(gomock.Any(), "localstack-aws", gomock.Any(), false).
		DoAndReturn(func(_ context.Context, _ string, out io.Writer, _ bool) error {
			_, err := io.WriteString(out, content)
			return err
		})

	sink := &captureSink{}
	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	err := Logs(context.Background(), mockRT, sink, containers, false, verbose)
	require.NoError(t, err)
	return sink
}

// A single log line longer than maxLogLineBytes must not error (regression for
// "bufio.Scanner: token too long") and must be emitted truncated, not buffered
// whole in memory.
func TestLogs_LongLineIsTruncatedNotErrored(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("x", maxLogLineBytes+50_000)
	line := "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : " + huge + "\n"

	sink := runLogs(t, line, false)

	require.Len(t, sink.lines, 1)
	emitted := sink.lines[0].Line
	assert.LessOrEqual(t, len(emitted), maxLogLineBytes+64, "emitted line must be bounded near the cap")
	assert.Contains(t, emitted, "truncated")
	assert.Equal(t, output.LogLevelInfo, sink.lines[0].Level, "level still parsed from the prefix")
}

func TestLogs_NormalLinesPassThrough(t *testing.T) {
	t.Parallel()
	content := "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : hello\n" +
		"2026-07-07T10:05:12.240  WARN --- [  MainThread] l.bar : world\n"

	sink := runLogs(t, content, false)

	require.Len(t, sink.lines, 2)
	assert.Equal(t, "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : hello", sink.lines[0].Line)
	assert.Equal(t, output.LogLevelInfo, sink.lines[0].Level)
	assert.Equal(t, output.LogLevelWarn, sink.lines[1].Level)
}

// A final line without a trailing newline must still be emitted.
func TestLogs_NoTrailingNewline(t *testing.T) {
	t.Parallel()
	sink := runLogs(t, "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : tail", false)

	require.Len(t, sink.lines, 1)
	assert.Equal(t, "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : tail", sink.lines[0].Line)
}

func TestLogs_FilteredLinesDropped(t *testing.T) {
	t.Parallel()
	content := "2026-07-07T10:05:11.240  INFO --- [  MainThread] localstack.request.http : noise\n" +
		"2026-07-07T10:05:12.240  INFO --- [  MainThread] l.foo : keep\n"

	sink := runLogs(t, content, false)

	require.Len(t, sink.lines, 1)
	assert.Contains(t, sink.lines[0].Line, "keep")
}

// When no matching emulator container is running, Logs must fail with a
// silent error instead of trying to stream from a nonexistent container.
func TestLogs_EmulatorNotRunning_ReturnsSilentError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().
		FindRunningByImage(gomock.Any(), []string{"localstack/localstack-pro", "localstack/localstack"}, "4566/tcp").
		Return(nil, nil)

	sink := &captureSink{}
	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	err := Logs(context.Background(), mockRT, sink, containers, false, false)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "error must be silent since the sink already surfaced a 'not running' message to the user")
}
