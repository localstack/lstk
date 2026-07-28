package integration_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
)

func TestExternalPodOperationsRequireCallerAuthentication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "load", args: []string{"snapshot", "load", "pod:my-baseline"}},
		{name: "dry run", args: []string{"snapshot", "load", "--dry-run", "pod:my-baseline"}},
		{name: "save", args: []string{"snapshot", "save", "pod:my-baseline"}},
		{name: "remove", args: []string{"snapshot", "remove", "pod:my-baseline", "--force"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var podCalls atomic.Int32
			health := awsHealthHandler()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/_localstack/pods") {
					podCalls.Add(1)
					w.WriteHeader(http.StatusOK)
					return
				}
				health.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)

			environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
				Without(env.AuthToken).
				With(env.DisableEvents, "1")
			args := append([]string{"--non-interactive", "--endpoint-url", srv.URL}, tc.args...)

			_, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, args...)
			requireExitCode(t, 1, err)
			assert.Contains(t, stderr, "authentication is required for cloud snapshot operations against an externally-managed emulator")
			assert.Zero(t, podCalls.Load(), "the protected pod endpoint must not be called without caller authentication")
		})
	}
}
