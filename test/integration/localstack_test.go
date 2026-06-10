package integration_test

import (
	"context"
	"os"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
)

// Helpers for e2e tests that need a *real* LocalStack — one that serves the AWS
// APIs — rather than the alpine stand-in (startTestContainer) most integration
// tests use for discovery/health only. Keep these reusable across e2e suites
// (terraform, and future ones) instead of duplicating the bring-up logic.

// requireAuthToken returns LOCALSTACK_AUTH_TOKEN or skips the test. The real
// LocalStack image needs it to activate, so these tests can't run without it.
func requireAuthToken(t *testing.T) string {
	t.Helper()
	token := env.Get(env.AuthToken)
	if token == "" {
		t.Skip("LOCALSTACK_AUTH_TOKEN not set")
	}
	return token
}

// startRealLocalStack brings up a real LocalStack AWS emulator via `lstk start`
// — the same path real users take — rather than driving the Docker SDK directly.
// That gives us, for free, exactly what these e2e tests need: lstk resolves its
// default AWS image (localstack/localstack-pro:latest), binds the edge port 4566
// to 127.0.0.1 (the address lstk resolves the endpoint to, so a host-side
// subprocess like terraform can reach it), names the container so lstk's own
// discovery finds it, activates the image with the auth token, and blocks until
// the health endpoint is ready before returning. The caller is responsible for
// removing the container (e.g. t.Cleanup(cleanup), which removes "localstack-aws").
//
// lstk start bind-mounts a persistence/cache volume under $HOME/.cache that
// LocalStack — running as root in the container — writes root-owned files into.
// We therefore give lstk an isolated HOME created with os.MkdirTemp rather than
// t.TempDir: t.TempDir's automatic RemoveAll runs as the non-root test user and
// would fail the test trying to delete those root-owned files. Cleanup here is
// best-effort instead; leftovers in a temp dir are harmless (persistence is off,
// so the volume holds only LocalStack's cache, never test resource state).
func startRealLocalStack(t *testing.T, ctx context.Context, token string) {
	t.Helper()
	home, err := os.MkdirTemp("", "lstk-e2e-home")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })

	e := env.With(env.DisableEvents, "1").With(env.Home, home).With(env.AuthToken, token)
	_, stderr, err := runLstk(t, ctx, "", e, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
}
