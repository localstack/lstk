package integration_test

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDockerPullServer is a minimal Docker Engine API that reports no
// containers or images and answers POST /images/create by streaming a
// recorded pull, so the TUI pull progress can be tested without a daemon.
type fakeDockerPullServer struct {
	*httptest.Server
	mu     sync.Mutex
	pulled bool
}

var apiVersionPrefix = regexp.MustCompile(`^/v[0-9.]+`)

func newFakeDockerPullServer(t *testing.T, stream []byte, delay time.Duration) *fakeDockerPullServer {
	t.Helper()
	f := &fakeDockerPullServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := apiVersionPrefix.ReplaceAllString(r.URL.Path, "")
		w.Header().Set("API-Version", "1.52")
		w.Header().Set("OSType", "linux")
		switch {
		case path == "/_ping":
			_, _ = w.Write([]byte("OK"))
		case path == "/version":
			_, _ = w.Write([]byte(`{"ApiVersion":"1.52","MinAPIVersion":"1.24","Version":"29.0.0","Os":"linux","Arch":"arm64"}`))
		case path == "/info":
			_, _ = w.Write([]byte(`{"Name":"fake","OSType":"linux","OperatingSystem":"fake"}`))
		case path == "/containers/json", path == "/images/json":
			_, _ = w.Write([]byte(`[]`))
		case path == "/images/create" && r.Method == http.MethodPost:
			f.mu.Lock()
			f.pulled = true
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			flusher, _ := w.(http.Flusher)
			sc := bufio.NewScanner(bytes.NewReader(stream))
			for sc.Scan() {
				_, _ = w.Write(append(sc.Bytes(), '\n'))
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(delay)
			}
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"message":"fake docker: no such object: %s"}`, path)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeDockerPullServer) wasPulled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pulled
}

func freeTCPPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// renderedPullBar matches one rendered frame of the pull progress line, e.g.
// "Downloading  2/4  layers  ━━━━───  42%".
var renderedPullBar = regexp.MustCompile(`(\d+)/(\d+)\s+layers\s+[━─]+\s+(\d+)%`)

// TestStartPullProgressNeverMovesBackward replays a real Docker Desktop pull
// through `lstk start` and checks every rendered frame: the bar must never
// move backward and the layer counter must only count real layers (DEVX-1114).
func TestStartPullProgressNeverMovesBackward(t *testing.T) {
	t.Parallel()

	stream, err := os.ReadFile(filepath.Join("testdata", "pull_containerd_python.jsonl"))
	require.NoError(t, err)
	docker := newFakeDockerPullServer(t, stream, 30*time.Millisecond)

	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(fmt.Sprintf(`
[[containers]]
type = "aws"
tag = "latest"
port = %q
image = "lstk-pull-progress-fixture"
`, freeTCPPort(t))), 0644))

	// The "latest" tag defers the license check until after the pull; the
	// unreachable API endpoint then ends the run once the pull has rendered.
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AuthToken, "dummy-token").
		With(env.APIEndpoint, "http://127.0.0.1:1").
		With(env.DisableEvents, "1")
	e = append(e, "DOCKER_HOST=tcp://"+strings.TrimPrefix(docker.URL, "http://"))

	out, _ := runLstkInPTY(t, testContext(t), e, "start", "--config", configFile)
	require.True(t, docker.wasPulled(), "lstk never requested the pull; output:\n%s", out)

	frames := renderedPullBar.FindAllStringSubmatch(out, -1)
	require.GreaterOrEqual(t, len(frames), 5, "expected several rendered progress frames; output:\n%s", out)

	// The fixture announces 4 layers; its config/index blobs must not count.
	prev := 0
	for i, f := range frames {
		layers, err := strconv.Atoi(f[2])
		require.NoError(t, err)
		assert.LessOrEqual(t, layers, 4, "frame %d counts non-layer blobs: %q", i, f[0])
		pct, err := strconv.Atoi(f[3])
		require.NoError(t, err)
		assert.GreaterOrEqual(t, pct, prev, "frame %d moved backward: %q", i, f[0])
		prev = pct
	}
	last := frames[len(frames)-1]
	assert.Equal(t, "4/4", last[1]+"/"+last[2], "pull should end with all layers done: %q", last[0])
}
