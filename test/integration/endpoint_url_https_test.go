package integration_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalStateZip builds a minimal valid state export ZIP, mirroring
// mockStateServer's inline construction in snapshot_save_test.go.
func minimalStateZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("state.json")
	require.NoError(t, err)
	_, err = f.Write([]byte(`{"services":{}}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// httpsCertFileEnv writes srv's certificate to a temp PEM file and returns the
// "SSL_CERT_FILE=..." entry that makes the lstk subprocess trust it. lstk
// itself has no --insecure/trust-this-cert flag (a deliberate Non-Goal — see
// design.md), so the only way an exec'd subprocess can trust a self-signed
// httptest.NewTLSServer cert is via the OS-level trusted-root mechanism its
// TLS stack actually reads at verification time. Go's crypto/x509 honors
// SSL_CERT_FILE only on the unix builds it lists in root_unix.go (linux,
// freebsd, netbsd, openbsd, dragonfly) — darwin's cgo-based verifier goes
// through Security.framework instead and never consults it, and Windows uses
// its own store. So these tests only run on Linux; elsewhere they'd either
// skip silently or falsely fail on an OS-trust-store limitation that has
// nothing to do with the code under test.
func httpsCertFileEnv(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))
	return "SSL_CERT_FILE=" + path
}

func requireLinuxForSSLCertFileTrust(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("SSL_CERT_FILE trust for a self-signed httptest cert is only honored by Go's Linux x509 verifier; see httpsCertFileEnv")
	}
}

// awsHealthTLSServer is awsHealthServer's https counterpart, simulating a
// LocalStack cloud-hosted ephemeral instance. It serves the same handler, so
// every route the http mock answers is answered here too.
func awsHealthTLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(awsHealthHandler())
}

// TestAWSCommandEndpointURLHTTPSNoDockerRequired mirrors
// TestAWSCommandEndpointURLNoDockerRequired but proves the resolved https
// scheme reaches the wrapped aws binary unchanged rather than being
// downgraded to http (Decision 7 / task 7.2).
func TestAWSCommandEndpointURLHTTPSNoDockerRequired(t *testing.T) {
	t.Parallel()
	requireLinuxForSSLCertFileTrust(t)

	srv := awsHealthTLSServer(t)
	defer srv.Close()

	fakeDir := writeFakeAWSEcho(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost, httpsCertFileEnv(t, srv))

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "aws", "s3", "ls")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ENDPOINT:"+srv.URL)
	assert.Contains(t, stdout, "ARGS:s3 ls")
}

// TestCDKCommandEndpointURLHTTPSNoDockerRequired mirrors
// TestCDKAWSEndpointURLBypassesDockerCheck's shape but for an explicit
// --endpoint-url https:// target, proving the IaC proxy layer forwards the
// real scheme to the wrapped tool's environment instead of reconstructing
// "http://"+host.
func TestCDKCommandEndpointURLHTTPSNoDockerRequired(t *testing.T) {
	t.Parallel()
	requireLinuxForSSLCertFileTrust(t)

	srv := awsHealthTLSServer(t)
	defer srv.Close()

	fakeDir := writeFakeCDK(t, "2.177.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost, httpsCertFileEnv(t, srv))

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "cdk", "deploy", "MyStack")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ENV_AWS_ENDPOINT_URL="+srv.URL)
}

// TestSnapshotSaveEndpointURLHTTPSNoDockerRequired proves the emulator-client
// layer (internal/emulator/aws/client.go's ExportState) reaches an
// externally-managed https endpoint directly, without downgrading to http
// (task 7.3).
func TestSnapshotSaveEndpointURLHTTPSNoDockerRequired(t *testing.T) {
	t.Parallel()
	requireLinuxForSSLCertFileTrust(t)

	var gotPath string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version":  "3.0.2",
				"services": map[string]string{"s3": "available"},
			})
		case "/_localstack/pods/state":
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/zip")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(minimalStateZip(t))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost, httpsCertFileEnv(t, srv))

	stdout, stderr, err := runLstk(t, testContext(t), dir, e, "--endpoint-url", srv.URL, "snapshot", "save")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")
	assert.Equal(t, "/_localstack/pods/state", gotPath)
}

// TestStatusEndpointURLHTTPSchemeMismatchSuggestsHTTPS covers the confusing
// error a user hit when pointing at a cloud-hosted instance with `http://`:
// nothing listens on port 80 there, so the only thing reported was a raw "no
// route to host" that never hinted the scheme was the problem. lstk now
// re-probes the other scheme on the failure path and names the URL to retry.
func TestStatusEndpointURLHTTPSchemeMismatchSuggestsHTTPS(t *testing.T) {
	t.Parallel()
	requireLinuxForSSLCertFileTrust(t)

	srv := awsHealthTLSServer(t)
	defer srv.Close()

	httpURL := strings.Replace(srv.URL, "https://", "http://", 1)
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost, httpsCertFileEnv(t, srv))

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", httpURL, "status")
	require.Error(t, err)
	assert.Contains(t, stderr, "could not reach LocalStack emulator at "+httpURL)
	assert.Contains(t, stderr, srv.URL+" responded")
	assert.Contains(t, stderr, "retry with that URL")
	assert.NotContains(t, stderr, "lstk start")
}

// TestStatusEndpointURLHTTPSSchemeMismatchSuggestsHTTP is the mirror image:
// an `https://` URL aimed at a plain-HTTP emulator fails on the TLS handshake,
// which is just as opaque, and gets the same treatment. No certificate trust
// is involved, so unlike its siblings here this one runs on every platform.
func TestStatusEndpointURLHTTPSSchemeMismatchSuggestsHTTP(t *testing.T) {
	t.Parallel()

	srv := awsHealthServer(t)
	defer srv.Close()

	httpsURL := strings.Replace(srv.URL, "http://", "https://", 1)
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", httpsURL, "status")
	require.Error(t, err)
	assert.Contains(t, stderr, "could not reach LocalStack emulator at "+httpsURL)
	assert.Contains(t, stderr, srv.URL+" responded")
	assert.Contains(t, stderr, "retry with that URL")
}

// TestStatusEndpointURLHTTPSRendersReducedOutput is the https counterpart of
// TestStatusEndpointURLRendersReducedOutput (task 7.5).
func TestStatusEndpointURLHTTPSRendersReducedOutput(t *testing.T) {
	t.Parallel()
	requireLinuxForSSLCertFileTrust(t)

	srv := awsHealthTLSServer(t)
	defer srv.Close()

	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost, httpsCertFileEnv(t, srv))

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "status")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "running")
	assert.Contains(t, stdout, srv.URL)
	assert.Contains(t, stdout, "3.0.2")
	assert.NotContains(t, stdout, "Container:")
	assert.NotContains(t, stdout, "Uptime:")
}
