package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type customImageCreateRequest struct {
	Image      string
	Env        []string
	HostConfig struct{ Binds []string }
}

// The image is absent (or an older unbundled build) until pulled. Stop at the
// Docker create request so these tests exercise the real CLI's auth, pull, and
// license-mount decisions without requiring an Enterprise image or a registry.
func customImageDockerServer(t *testing.T, image, tag string, bundled, local bool) (*httptest.Server, *atomic.Int32, <-chan customImageCreateRequest) {
	t.Helper()
	var pulls atomic.Int32
	created := make(chan customImageCreateRequest, 1)
	imageEnv := []string{"LOCALSTACK_BUILD_VERSION=2026.8.4"}
	if bundled {
		imageEnv = append(imageEnv, "LOCALSTACK_AUTH_TOKEN_OVERRIDE=bundled-test-token")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := apiVersionPrefix.ReplaceAllString(r.URL.Path, "")
		w.Header().Set("API-Version", "1.52")
		w.Header().Set("OSType", "linux")
		w.Header().Set("Content-Type", "application/json")
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
			assert.Equal(t, strings.TrimSuffix(image, ":"+tag), r.URL.Query().Get("fromImage"))
			assert.Equal(t, tag, r.URL.Query().Get("tag"))
			pulls.Add(1)
			_, _ = w.Write([]byte(`{"status":"Downloaded newer image"}` + "\n"))
		case strings.HasPrefix(path, "/images/") && strings.HasSuffix(path, "/json") && (local || pulls.Load() > 0):
			currentEnv := imageEnv
			if pulls.Load() == 0 {
				currentEnv = []string{"LOCALSTACK_BUILD_VERSION=2026.8.4"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "sha256:custom-image", "Config": map[string]any{"Env": currentEnv}})
		case path == "/containers/create" && r.Method == http.MethodPost:
			var req customImageCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode Docker create request: %v", err)
			} else {
				created <- req
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"fixture reached emulator creation"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such object"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &pulls, created
}

func TestStartResolvesCustomImageBeforeAuthentication(t *testing.T) {
	requireServicePortsFree(t)
	for _, tc := range []struct {
		name        string
		tag         string
		bundled     bool
		storedToken string
		imageFlag   bool
		local       bool
	}{
		{name: "bundled image without a token", tag: "latest", bundled: true, imageFlag: true},
		{name: "pinned bundled image with a stale stored token", tag: "2026.8.4", bundled: true, storedToken: "stale-token"},
		{name: "floating bundled image with a stale stored token", tag: "latest", bundled: true, storedToken: "stale-token"},
		{name: "moving image gains bundled license after pull", tag: "latest", bundled: true, storedToken: "stale-token", local: true},
		{name: "ordinary image still requires a token", tag: "latest", imageFlag: true},
		{name: "ordinary pinned image validates stored token after pull", tag: "2026.8.4", storedToken: "stale-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const imageRepo = "registry.example.com/localstack-test"
			image := imageRepo + ":" + tc.tag
			docker, pulls, created := customImageDockerServer(t, image, tc.tag, tc.bundled, tc.local)
			e, configFile, platformHits := bundledLicenseEnv(t)
			home := filepath.Dir(configFile)
			if tc.storedToken != "" {
				// --config makes its parent directory the file-keyring location.
				require.NoError(t, os.WriteFile(filepath.Join(home, "auth-token"), []byte(tc.storedToken), 0600))
			}
			configImage := imageRepo
			args := []string{"--config", configFile, "--non-interactive", "start"}
			if tc.imageFlag {
				configImage = ""
				args = append(args, "--image", image)
			}
			configContent := fmt.Sprintf("[[containers]]\ntype = \"aws\"\ntag = %q\nport = %q\nimage = %q\nvolume = %q\n", tc.tag, freeTCPPort(t), configImage, filepath.Join(home, "volume"))
			require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))
			e = e.With("DOCKER_HOST", "tcp://"+strings.TrimPrefix(docker.URL, "http://")).
				With("LSTK_CHECK_FOR_UPDATE_ON_STARTUP", "false").
				Without("DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH")

			stdout, stderr, err := runLstk(t, testContext(t), "", e, args...)
			combined := stdout + stderr
			require.Error(t, err, "the fixture intentionally ends at auth or emulator creation")
			requireExitCode(t, 1, err)
			assert.Equal(t, int32(1), pulls.Load(), "the custom image must be pulled exactly once: %s", combined)

			if !tc.bundled {
				if tc.storedToken != "" {
					assert.Equal(t, int32(1), atomic.LoadInt32(platformHits), "a newly pulled ordinary image still validates its token")
					assert.Contains(t, combined, "License validation failed")
				} else {
					assert.Equal(t, int32(0), atomic.LoadInt32(platformHits))
					assert.Contains(t, combined, "authentication required")
				}
				select {
				case <-created:
					t.Error("ordinary image must not start without a valid token")
				default:
				}
				return
			}

			assert.Equal(t, int32(0), atomic.LoadInt32(platformHits), "a bundled license must not trigger a platform request")
			assert.NotContains(t, combined, "Checking license")
			assert.Contains(t, combined, "fixture reached emulator creation")
			assert.NotContains(t, combined, "authentication required")
			select {
			case req := <-created:
				assert.Equal(t, image, req.Image)
				assert.NotContains(t, req.Env, "LOCALSTACK_AUTH_TOKEN=", "do not shadow an image token with an empty value")
				if tc.storedToken != "" {
					assert.Contains(t, req.Env, "LOCALSTACK_AUTH_TOKEN="+tc.storedToken, "the regression must exercise an existing saved token")
				}
				for _, bind := range req.HostConfig.Binds {
					assert.NotContains(t, bind, ":/etc/localstack/conf.d/license.json", "a cached license must not replace the bundled license")
				}
			default:
				t.Fatalf("lstk did not attempt to create the emulator: %s", combined)
			}
		})
	}
}
