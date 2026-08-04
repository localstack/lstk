package integration_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStatusCommandShowsResourcesWhenRunning and TestStatusCommandWorksWithNonDefaultPort
// are the only cases kept here: the rest of the original test/integration/status_test.go
// was ported to test/e2e/tests/status.test.ts. These two were not:
//   - this one needs a real AWS SDK client to create S3/SQS resources so `lstk status`
//     has something to report — not just a mock health/resources HTTP endpoint.
//   - TestStatusCommandWorksWithNonDefaultPort (below) publishes on the 127.0.0.2
//     loopback alias, which Docker Desktop's VM-backed networking rejects but a
//     native Linux daemon (as used in CI) accepts.

func TestStatusCommandShowsResourcesWhenRunning(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		awsconfig.WithBaseEndpoint("http://localhost:4566"),
	)
	require.NoError(t, err)

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) { o.UsePathStyle = true })
	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("my-test-bucket")})
	require.NoError(t, err, "failed to create S3 bucket")

	sqsClient := sqs.NewFromConfig(awsCfg)
	_, err = sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String("my-test-queue")})
	require.NoError(t, err, "failed to create SQS queue")

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "status")
	require.NoError(t, err, "lstk status failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "running")
	assert.Contains(t, stdout, "SERVICE")
	assert.Contains(t, stdout, "RESOURCE")
	assert.Contains(t, stdout, "S3")
	assert.Contains(t, stdout, "my-test-bucket")
	assert.Contains(t, stdout, "SQS")
	assert.Contains(t, stdout, "my-test-queue")
	assertCommandTelemetry(t, events, "status", 0)
}

func TestStatusCommandWorksWithNonDefaultPort(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	// The mock server is assigned a random free port (guaranteed not to conflict).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"version": "4.14.1", "services": {}}`)
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Extract the port so we can bind it to the container.
	_, mockPort, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)

	// Simulates starting LocalStack on a non-default host port.
	startTestContainer(t, ctx, mockPort)

	// Write a config with the default port 4566
	// Simulates the user changing the config port after starting the container
	configContent := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "--config", configFile, "status")
	require.NoError(t, err, "lstk status failed: %s", stderr)
	assert.Contains(t, stdout, "4.14.1")
}
