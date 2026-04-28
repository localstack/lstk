package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/docker/docker/api/types/image"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, _, err := runLstk(t, testContext(t), "", envWithDockerHost(t, daemon).With(env.AnalyticsEndpoint, analyticsSrv.URL), "status")
	require.Error(t, err, "expected lstk status to fail when emulator not running")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack:")
	assert.Contains(t, stdout, "See help:")
	assertCommandTelemetry(t, events, "status", 1)
}

func TestStatusCommandShowsResourcesWhenRunning(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)

	ctx := testContext(t)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	_, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)

	awsEndpoint := fmt.Sprintf("http://127.0.0.1:%d", daemon.hostPortFor(4566))
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		awsconfig.WithBaseEndpoint(awsEndpoint),
	)
	require.NoError(t, err)

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) { o.UsePathStyle = true })
	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("my-test-bucket")})
	require.NoError(t, err, "failed to create S3 bucket")

	sqsClient := sqs.NewFromConfig(awsCfg)
	_, err = sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String("my-test-queue")})
	require.NoError(t, err, "failed to create SQS queue")

	analyticsSrv, events := mockAnalyticsServer(t)
	// LOCALSTACK_HOST tells lstk status which host:port to query.
	host := fmt.Sprintf("127.0.0.1:%d", daemon.hostPortFor(4566))
	stdout, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.AnalyticsEndpoint, analyticsSrv.URL).With(env.LocalStackHost, host), "status")
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
	// Hard: lstk status reads the container's host-port binding via Docker
	// inspect and then issues an HTTP request against that port from the test
	// process. With dind, the container's port lives inside dind's namespace
	// and can't be reached from the test process directly.
	t.Skip("TODO: rewrite for dind — needs port forwarding from dind to host")
}

func TestStatusCommandWorksWithExternalContainer(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t)
	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	require.NoError(t, daemon.Client.ImageTag(ctx, testImage, fakeImage))
	t.Cleanup(func() {
		_, _ = daemon.Client.ImageRemove(context.Background(), fakeImage, image.RemoveOptions{})
	})

	startExternalInDind(t, daemon, fakeImage, "localstack-external", "4566")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"version": "3.5.0", "services": {}}`)
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")

	stdout, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.LocalStackHost, host), "status")
	require.NoError(t, err, "lstk status should work with external container: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "3.5.0")
}

func TestStatusCommandForSnowflakeShowsNoResources(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t)
	ctx := testContext(t)
	startStubInDind(t, daemon, snowflakeContainerName)

	stdout, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon), "--config", writeSnowflakeConfig(t, "4566"), "status")
	require.NoError(t, err, "lstk status failed for snowflake: %s", stderr)
	requireExitCode(t, 0, err)

	assert.Contains(t, stdout, "Snowflake")
	assert.Contains(t, stdout, "running")
	// Snowflake does not expose AWS resources — no resource table or empty-state message.
	assert.NotContains(t, stdout, "SERVICE")
	assert.NotContains(t, stdout, "No resources deployed")
}

func TestStatusCommandShowsNoResourcesWhenEmpty(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t)
	ctx := testContext(t)
	startStubInDind(t, daemon, containerName)

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

	host := strings.TrimPrefix(server.URL, "http://")

	stdout, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.LocalStackHost, host), "status")
	require.NoError(t, err, "lstk status failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "No resources deployed")
}
