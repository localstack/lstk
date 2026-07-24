package mcpconfig

import (
	"context"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingSink captures emitted events for assertions.
type recordingSink struct{ events []output.Event }

func (s *recordingSink) Emit(e output.Event) { s.events = append(s.events, e) }

func (s *recordingSink) messages() []output.MessageEvent {
	var out []output.MessageEvent
	for _, e := range s.events {
		if m, ok := e.(output.MessageEvent); ok {
			out = append(out, m)
		}
	}
	return out
}

func (s *recordingSink) hasError() bool {
	for _, e := range s.events {
		if _, ok := e.(output.ErrorEvent); ok {
			return true
		}
	}
	return false
}

// fakeAdapter records Install calls and reports a fixed detection/outcome.
type fakeAdapter struct {
	id          string
	installed   bool
	unsupported string
	outcome     string
	installCnt  int
}

func (f *fakeAdapter) ID() string                          { return f.id }
func (f *fakeAdapter) Label() string                       { return f.id }
func (f *fakeAdapter) Detect(ClientContext) (bool, string) { return f.installed, f.unsupported }
func (f *fakeAdapter) Install(context.Context, ServerSpec, ClientContext) InstallOutcome {
	f.installCnt++
	status := f.outcome
	if status == "" {
		status = statusInstalled
	}
	return InstallOutcome{status, "ok"}
}

func TestRunInitRequiresToken(t *testing.T) {
	sink := &recordingSink{}
	err := runInit(context.Background(), sink, Options{Method: MethodDocker}, nil, ClientContext{})

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "missing-token error should be silent (already emitted)")
	assert.True(t, sink.hasError())
}

func TestRunInitAutoDetectInstallsOnlyDetected(t *testing.T) {
	sink := &recordingSink{}
	a := &fakeAdapter{id: "a", installed: true}
	b := &fakeAdapter{id: "b", installed: false}
	c := &fakeAdapter{id: "c", installed: true, unsupported: "nope"}

	err := runInit(context.Background(), sink, Options{Token: "ls-x", Method: MethodNPX},
		[]ClientAdapter{a, b, c}, ClientContext{})
	require.NoError(t, err)

	assert.Equal(t, 1, a.installCnt, "detected adapter installed")
	assert.Equal(t, 0, b.installCnt, "undetected adapter skipped")
	assert.Equal(t, 0, c.installCnt, "unsupported adapter skipped")
}

func TestRunInitExplicitClientsBypassDetection(t *testing.T) {
	sink := &recordingSink{}
	a := &fakeAdapter{id: "a", installed: false}
	b := &fakeAdapter{id: "b", installed: false}

	err := runInit(context.Background(), sink, Options{Token: "ls-x", Method: MethodNPX, ClientIDs: []string{"b"}},
		[]ClientAdapter{a, b}, ClientContext{})
	require.NoError(t, err)

	assert.Equal(t, 0, a.installCnt)
	assert.Equal(t, 1, b.installCnt, "explicitly requested client installed even if undetected")
}

func TestRunInitUnknownClientErrors(t *testing.T) {
	sink := &recordingSink{}
	a := &fakeAdapter{id: "a", installed: true}

	err := runInit(context.Background(), sink, Options{Token: "ls-x", ClientIDs: []string{"nope"}},
		[]ClientAdapter{a}, ClientContext{})
	require.Error(t, err)
	assert.True(t, sink.hasError())
}

func TestRunInitNoTargetsEmitsNote(t *testing.T) {
	sink := &recordingSink{}
	a := &fakeAdapter{id: "a", installed: false}

	err := runInit(context.Background(), sink, Options{Token: "ls-x", Method: MethodNPX},
		[]ClientAdapter{a}, ClientContext{})
	require.NoError(t, err)

	var sawNote bool
	for _, m := range sink.messages() {
		if m.Severity == output.SeverityNote {
			sawNote = true
		}
	}
	assert.True(t, sawNote, "should advise when no clients detected")
}

func TestRunInitAllFailedReturnsError(t *testing.T) {
	sink := &recordingSink{}
	a := &fakeAdapter{id: "a", installed: true, outcome: statusFailed}

	err := runInit(context.Background(), sink, Options{Token: "ls-x", Method: MethodNPX},
		[]ClientAdapter{a}, ClientContext{})
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}

func TestBuildSpecUnknownMethod(t *testing.T) {
	_, err := buildSpec(Options{Token: "x", Method: "bogus"})
	assert.Error(t, err)
}

func TestBuildSpecDefaultsToDocker(t *testing.T) {
	spec, err := buildSpec(Options{Token: "x", Docker: DockerOptions{CacheDir: "/c", ImageTag: "latest"}})
	require.NoError(t, err)
	assert.Equal(t, "docker", spec.Command)
}
