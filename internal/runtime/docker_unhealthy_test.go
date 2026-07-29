package runtime

import (
	"errors"
	"os"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/internal/output"
)

// captureSink records every event emitted so tests can assert on the resulting
// ErrorEvent's action list.
type captureSink struct {
	events []output.Event
}

func (s *captureSink) Emit(event output.Event) {
	s.events = append(s.events, event)
}

func (s *captureSink) errorEvent(t *testing.T) output.ErrorEvent {
	t.Helper()
	require.Len(t, s.events, 1)
	errEvent, ok := s.events[0].(output.ErrorEvent)
	require.True(t, ok, "expected an ErrorEvent, got %T", s.events[0])
	return errEvent
}

func notFound(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
func notOnPath(string) (string, error)     { return "", errors.New("not found") }
func emptyGetenv(string) string            { return "" }
func newUnhealthyRuntime(t *testing.T, host string) *DockerRuntime {
	t.Helper()
	cli, err := client.New(client.WithHost(host))
	require.NoError(t, err)
	return &DockerRuntime{client: cli}
}

func TestEmitUnhealthyError_TitleStaysStable(t *testing.T) {
	rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")
	sink := &captureSink{}

	rt.emitUnhealthyError(sink, errors.New("boom"), "/home/user", notFound, notOnPath, emptyGetenv, "linux")

	assert.Equal(t, "Docker is not available", sink.errorEvent(t).Title)
}

func TestEmitUnhealthyError_ConfiguredRancherSocketWinsFirstAction(t *testing.T) {
	home := "/home/user"
	rt := newUnhealthyRuntime(t, "unix://"+home+"/.rd/docker.sock")
	sink := &captureSink{}

	rt.emitUnhealthyError(sink, errors.New("boom"), home, notFound, notOnPath, emptyGetenv, "darwin")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Rancher Desktop:", actions[0].Label)
	assert.Equal(t, "rdctl start", actions[0].Value)
}

func TestEmitUnhealthyError_RancherHomeDirEvidenceWinsWithoutSocket(t *testing.T) {
	home := "/home/user"
	// Daemon host is the plain default socket (unresolved), so evidence must come
	// from the filesystem instead of the configured socket path.
	rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")
	sink := &captureSink{}

	statFn := func(path string) (os.FileInfo, error) {
		if path == home+"/.rd" {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	rt.emitUnhealthyError(sink, errors.New("boom"), home, statFn, notOnPath, emptyGetenv, "linux")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Rancher Desktop:", actions[0].Label)
	assert.Equal(t, "rdctl start", actions[0].Value)
}

func TestEmitUnhealthyError_PodmanOnPath_Darwin(t *testing.T) {
	home := "/home/user"
	rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")
	sink := &captureSink{}

	lookPath := func(name string) (string, error) {
		if name == "podman" {
			return "/opt/homebrew/bin/podman", nil
		}
		return "", errors.New("not found")
	}

	rt.emitUnhealthyError(sink, errors.New("boom"), home, notFound, lookPath, emptyGetenv, "darwin")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Podman:", actions[0].Label)
	assert.Equal(t, "podman machine start", actions[0].Value)
}

func TestEmitUnhealthyError_PodmanOnPath_Linux(t *testing.T) {
	home := "/home/user"
	rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")
	sink := &captureSink{}

	lookPath := func(name string) (string, error) {
		if name == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("not found")
	}

	rt.emitUnhealthyError(sink, errors.New("boom"), home, notFound, lookPath, emptyGetenv, "linux")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Podman:", actions[0].Label)
	assert.Equal(t, "systemctl --user start podman.socket", actions[0].Value)
}

func TestEmitUnhealthyError_ConfiguredNativeRootfulPodmanSocketWins(t *testing.T) {
	rt := newUnhealthyRuntime(t, "unix:///run/podman/podman.sock")
	sink := &captureSink{}
	t.Setenv("XDG_RUNTIME_DIR", "")

	rt.emitUnhealthyError(sink, errors.New("boom"), "/home/user", notFound, notOnPath, emptyGetenv, "linux")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Podman:", actions[0].Label)
	assert.Equal(t, "systemctl start podman", actions[0].Value, "rootful podman.sock must suggest the systemctl invocation without --user")
}

func TestEmitUnhealthyError_ConfiguredNativeRootlessPodmanSocketWins(t *testing.T) {
	xdgRuntimeDir := "/run/user/1000"
	rt := newUnhealthyRuntime(t, "unix://"+xdgRuntimeDir+"/podman/podman.sock")
	sink := &captureSink{}
	t.Setenv("XDG_RUNTIME_DIR", xdgRuntimeDir)

	rt.emitUnhealthyError(sink, errors.New("boom"), "/home/user", notFound, notOnPath, emptyGetenv, "linux")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Podman:", actions[0].Label)
	assert.Equal(t, "systemctl --user start podman.socket", actions[0].Value)
}

// A machine can have the podman CLI installed (e.g. as a side effect of some
// other tool) while actually running Colima; the stronger, install-specific
// evidence (Colima's state directory) must win over the mere presence of the
// podman binary on PATH.
func TestEmitUnhealthyError_ColimaHomeDirEvidenceWinsOverPodmanOnPath(t *testing.T) {
	home := "/home/user"
	rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")
	sink := &captureSink{}

	statFn := func(path string) (os.FileInfo, error) {
		if path == home+"/.colima" {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	lookPath := func(name string) (string, error) {
		if name == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("not found")
	}

	rt.emitUnhealthyError(sink, errors.New("boom"), home, statFn, lookPath, emptyGetenv, "linux")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Colima:", actions[0].Label)
	assert.Equal(t, "colima start", actions[0].Value)
}

func TestEmitUnhealthyError_ColimaHomeDirEvidence(t *testing.T) {
	home := "/home/user"
	rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")
	sink := &captureSink{}

	statFn := func(path string) (os.FileInfo, error) {
		if path == home+"/.colima" {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	rt.emitUnhealthyError(sink, errors.New("boom"), home, statFn, notOnPath, emptyGetenv, "darwin")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Colima:", actions[0].Label)
	assert.Equal(t, "colima start", actions[0].Value)
}

func TestEmitUnhealthyError_ColimaXDGConfigDirEvidence(t *testing.T) {
	home := "/home/user"
	rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")
	sink := &captureSink{}

	statFn := func(path string) (os.FileInfo, error) {
		if path == home+"/.config/colima" {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	rt.emitUnhealthyError(sink, errors.New("boom"), home, statFn, notOnPath, emptyGetenv, "linux")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Colima:", actions[0].Label)
}

func TestEmitUnhealthyError_OrbstackHomeDirEvidence_DarwinOnly(t *testing.T) {
	home := "/home/user"

	statFn := func(path string) (os.FileInfo, error) {
		if path == home+"/.orbstack" {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	t.Run("darwin gets tailored action", func(t *testing.T) {
		rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")
		sink := &captureSink{}
		rt.emitUnhealthyError(sink, errors.New("boom"), home, statFn, notOnPath, emptyGetenv, "darwin")

		actions := sink.errorEvent(t).Actions
		require.NotEmpty(t, actions)
		assert.Equal(t, "Start OrbStack:", actions[0].Label)
		assert.Equal(t, "open -a OrbStack", actions[0].Value)
	})

	t.Run("linux has no orbstack GUI hint, falls back to generic", func(t *testing.T) {
		rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")
		sink := &captureSink{}
		rt.emitUnhealthyError(sink, errors.New("boom"), home, statFn, notOnPath, emptyGetenv, "linux")

		actions := sink.errorEvent(t).Actions
		require.NotEmpty(t, actions)
		assert.Equal(t, "Start Docker:", actions[0].Label)
		assert.Equal(t, "sudo systemctl start docker", actions[0].Value)
	})
}

func TestEmitUnhealthyError_NoEvidenceFallsBackToGenericDockerActions(t *testing.T) {
	rt := newUnhealthyRuntime(t, "unix:///var/run/docker.sock")

	t.Run("darwin", func(t *testing.T) {
		sink := &captureSink{}
		rt.emitUnhealthyError(sink, errors.New("boom"), "/home/user", notFound, notOnPath, emptyGetenv, "darwin")
		actions := sink.errorEvent(t).Actions
		require.NotEmpty(t, actions)
		assert.Equal(t, "Start Docker Desktop:", actions[0].Label)
		assert.Equal(t, "open -a Docker", actions[0].Value)
	})

	t.Run("linux", func(t *testing.T) {
		sink := &captureSink{}
		rt.emitUnhealthyError(sink, errors.New("boom"), "/home/user", notFound, notOnPath, emptyGetenv, "linux")
		actions := sink.errorEvent(t).Actions
		require.NotEmpty(t, actions)
		assert.Equal(t, "Start Docker:", actions[0].Label)
		assert.Equal(t, "sudo systemctl start docker", actions[0].Value)
	})

	t.Run("always includes the install action as a fallback", func(t *testing.T) {
		sink := &captureSink{}
		rt.emitUnhealthyError(sink, errors.New("boom"), "/home/user", notFound, notOnPath, emptyGetenv, "linux")
		actions := sink.errorEvent(t).Actions
		assert.Contains(t, actions, output.ErrorAction{Label: "Install Docker:", Value: "https://docs.docker.com/get-docker/"})
	})
}

func TestEmitUnhealthyError_LimaFlavorHasNoTailoredActionFallsBackToGeneric(t *testing.T) {
	home := "/home/user"
	rt := newUnhealthyRuntime(t, "unix://"+home+"/.lima/docker/sock/docker.sock")
	sink := &captureSink{}

	rt.emitUnhealthyError(sink, errors.New("boom"), home, notFound, notOnPath, emptyGetenv, "darwin")

	actions := sink.errorEvent(t).Actions
	require.NotEmpty(t, actions)
	assert.Equal(t, "Start Docker Desktop:", actions[0].Label, "lima has no dedicated CLI start hint yet")
}
