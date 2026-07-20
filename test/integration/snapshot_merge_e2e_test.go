package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// End-to-end tests for `lstk snapshot load --merge` that exercise the real
// merge round-trip against a real LocalStack container (see
// localstack_test.go for the shared bring-up helpers) — unlike
// snapshot_load_test.go, which asserts against a mocked emulator and never
// exercises the actual account-region-merge/service-merge/overwrite logic
// (DEVX-946). Resources are created and inspected via `lstk aws`, the real aws
// CLI proxy, rather than the AWS SDK, so these tests drive lstk the same way a
// real user would.
//
// A single real container is reused across all three strategies via
// `lstk reset --force` between phases, rather than starting a fresh container
// per strategy: reset fully wipes all account/region/service state, which is
// functionally equivalent to a fresh emulator for this purpose, at a fraction
// of the cost.

func requireAWSCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not found on PATH")
	}
}

// createSNSTopic creates an SNS topic named name in region via `lstk aws`.
func createSNSTopic(t *testing.T, ctx context.Context, e env.Environ, region, name string) {
	t.Helper()
	_, stderr, err := runLstk(t, ctx, "", e, "aws", "sns", "create-topic", "--name", name, "--region", region)
	require.NoError(t, err, "create-topic %s/%s failed: %s", region, name, stderr)
}

// createSQSQueue creates an SQS queue named name in region via `lstk aws`,
// optionally with the given queue attributes (e.g. VisibilityTimeout).
func createSQSQueue(t *testing.T, ctx context.Context, e env.Environ, region, name string, attrs map[string]string) {
	t.Helper()
	args := []string{"aws", "sqs", "create-queue", "--queue-name", name, "--region", region}
	if len(attrs) > 0 {
		shorthand := ""
		for k, v := range attrs {
			if shorthand != "" {
				shorthand += ","
			}
			shorthand += k + "=" + v
		}
		args = append(args, "--attributes", shorthand)
	}
	_, stderr, err := runLstk(t, ctx, "", e, args...)
	require.NoError(t, err, "create-queue %s/%s failed: %s", region, name, stderr)
}

// createS3Bucket creates an S3 bucket named name in region via `lstk aws`.
func createS3Bucket(t *testing.T, ctx context.Context, e env.Environ, region, name string) {
	t.Helper()
	_, stderr, err := runLstk(t, ctx, "", e, "aws", "s3", "mb", "s3://"+name, "--region", region)
	require.NoError(t, err, "s3 mb %s/%s failed: %s", region, name, stderr)
}

// snsTopicNames lists the SNS topics in region and returns their names (the
// suffix after the last ':' in each topic ARN).
func snsTopicNames(t *testing.T, ctx context.Context, e env.Environ, region string) []string {
	t.Helper()
	stdout, stderr, err := runLstk(t, ctx, "", e, "aws", "sns", "list-topics", "--region", region, "--output", "json")
	require.NoError(t, err, "list-topics %s failed: %s", region, stderr)

	var resp struct {
		Topics []struct {
			TopicArn string `json:"TopicArn"`
		} `json:"Topics"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "parsing list-topics output: %s", stdout)

	names := make([]string, 0, len(resp.Topics))
	for _, topic := range resp.Topics {
		names = append(names, topicNameFromARN(topic.TopicArn))
	}
	return names
}

// topicNameFromARN extracts the topic name — the suffix after the last ':' —
// from an SNS topic ARN (arn:aws:sns:<region>:<account>:<name>).
func topicNameFromARN(arn string) string {
	idx := -1
	for i := len(arn) - 1; i >= 0; i-- {
		if arn[i] == ':' {
			idx = i
			break
		}
	}
	if idx == -1 {
		return arn
	}
	return arn[idx+1:]
}

// sqsQueueURLsByName lists the SQS queues in region and returns a map of queue
// name (the last path segment of the queue URL) to its full URL.
func sqsQueueURLsByName(t *testing.T, ctx context.Context, e env.Environ, region string) map[string]string {
	t.Helper()
	stdout, stderr, err := runLstk(t, ctx, "", e, "aws", "sqs", "list-queues", "--region", region, "--output", "json")
	require.NoError(t, err, "list-queues %s failed: %s", region, stderr)

	var resp struct {
		QueueUrls []string `json:"QueueUrls"`
	}
	if stdout != "" {
		require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "parsing list-queues output: %s", stdout)
	}

	byName := make(map[string]string, len(resp.QueueUrls))
	for _, url := range resp.QueueUrls {
		byName[path.Base(url)] = url
	}
	return byName
}

// sqsQueueVisibilityTimeout returns the VisibilityTimeout attribute of the
// queue at queueURL in region.
func sqsQueueVisibilityTimeout(t *testing.T, ctx context.Context, e env.Environ, region, queueURL string) string {
	t.Helper()
	stdout, stderr, err := runLstk(t, ctx, "", e, "aws", "sqs", "get-queue-attributes",
		"--queue-url", queueURL, "--attribute-names", "VisibilityTimeout", "--region", region, "--output", "json")
	require.NoError(t, err, "get-queue-attributes %s failed: %s", queueURL, stderr)

	var resp struct {
		Attributes struct {
			VisibilityTimeout string `json:"VisibilityTimeout"`
		} `json:"Attributes"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "parsing get-queue-attributes output: %s", stdout)
	return resp.Attributes.VisibilityTimeout
}

// s3BucketNames lists all S3 buckets and returns their names.
func s3BucketNames(t *testing.T, ctx context.Context, e env.Environ) []string {
	t.Helper()
	stdout, stderr, err := runLstk(t, ctx, "", e, "aws", "s3api", "list-buckets", "--output", "json")
	require.NoError(t, err, "list-buckets failed: %s", stderr)

	var resp struct {
		Buckets []struct {
			Name string `json:"Name"`
		} `json:"Buckets"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "parsing list-buckets output: %s", stdout)

	names := make([]string, len(resp.Buckets))
	for i, b := range resp.Buckets {
		names[i] = b.Name
	}
	return names
}

// buildMergeTestSnapshots creates snapshot1's resources (us-east-1 +
// ap-southeast-2), saves snapshot1, resets, creates snapshot2's resources
// (us-east-1 only), and saves snapshot2. Returns the two saved file paths.
func buildMergeTestSnapshots(t *testing.T, ctx context.Context, e env.Environ, dir string) (snap1, snap2 string) {
	t.Helper()

	createSNSTopic(t, ctx, e, "us-east-1", "topic1")
	createS3Bucket(t, ctx, e, "us-east-1", "bucket1")
	createSQSQueue(t, ctx, e, "us-east-1", "queue1", nil)
	createSQSQueue(t, ctx, e, "us-east-1", "queue-to-replace", map[string]string{"VisibilityTimeout": "30"})
	createSNSTopic(t, ctx, e, "ap-southeast-2", "topic2")
	createSQSQueue(t, ctx, e, "ap-southeast-2", "queue2", nil)

	snap1 = filepath.Join(dir, "snapshot1.snapshot")
	_, stderr, err := runLstk(t, ctx, "", e, "snapshot", "save", snap1)
	require.NoError(t, err, "snapshot save (1) failed: %s", stderr)

	_, stderr, err = runLstk(t, ctx, "", e, "reset", "--force")
	require.NoError(t, err, "reset before snapshot 2 failed: %s", stderr)

	createSNSTopic(t, ctx, e, "us-east-1", "topic3")
	createSQSQueue(t, ctx, e, "us-east-1", "queue3", nil)
	createSQSQueue(t, ctx, e, "us-east-1", "queue-to-replace", map[string]string{"VisibilityTimeout": "90"})

	snap2 = filepath.Join(dir, "snapshot2.snapshot")
	_, stderr, err = runLstk(t, ctx, "", e, "snapshot", "save", snap2)
	require.NoError(t, err, "snapshot save (2) failed: %s", stderr)

	return snap1, snap2
}

// mergeTestAWSTag pins the AWS emulator image tag for TestSnapshotLoadMergeStrategies
const mergeTestAWSTag = "dev"

func cleanupMergeTestContainer() {
	_, _ = dockerClient.ContainerRemove(context.Background(), "localstack-aws-"+mergeTestAWSTag, client.ContainerRemoveOptions{Force: true})
}

func TestSnapshotLoadMergeStrategies(t *testing.T) {
	requireDocker(t)
	requireAWSCLI(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	cleanupMergeTestContainer()
	t.Cleanup(cleanupMergeTestContainer)
	ctx := testContext(t)

	// Pinned to the "dev" image tag rather than the default "latest" so we can
	// catch problems with the merge logic in a known-good image before they hit users.
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("[[containers]]\ntype = \"aws\"\ntag = \""+mergeTestAWSTag+"\"\nport = \"4566\"\n"), 0644))
	startRealLocalStackWithConfig(t, ctx, token, configPath)

	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())

	// lstk aws prints a "No AWS profile found" note on stdout when no profile
	// exists, which would corrupt the --output json parsing below. Set one up
	// first (runLstk has no TTY, so this is non-interactive) so every later
	// `aws` call's stdout is clean JSON.
	_, stderr, err := runLstk(t, ctx, "", e, "setup", "aws")
	require.NoError(t, err, "lstk setup aws failed: %s", stderr)

	snap1, snap2 := buildMergeTestSnapshots(t, ctx, e, t.TempDir())

	tests := []struct {
		strategy         string
		wantUSEastSNS    []string
		wantUSEastSQS    []string
		wantUSEastS3     []string
		wantAPSoutheast2 map[string][]string // service -> resource names
		wantVisibility   string
	}{
		{
			strategy:         "overwrite",
			wantUSEastSNS:    []string{"topic3"},
			wantUSEastSQS:    []string{"queue3", "queue-to-replace"},
			wantUSEastS3:     nil,
			wantAPSoutheast2: map[string][]string{"sns": nil, "sqs": nil},
			wantVisibility:   "90",
		},
		{
			strategy:         "account-region-merge",
			wantUSEastSNS:    []string{"topic3"},
			wantUSEastSQS:    []string{"queue3", "queue-to-replace"},
			wantUSEastS3:     []string{"bucket1"},
			wantAPSoutheast2: map[string][]string{"sns": {"topic2"}, "sqs": {"queue2"}},
			wantVisibility:   "90",
		},
		{
			strategy:         "service-merge",
			wantUSEastSNS:    []string{"topic1", "topic3"},
			wantUSEastSQS:    []string{"queue1", "queue3", "queue-to-replace"},
			wantUSEastS3:     []string{"bucket1"},
			wantAPSoutheast2: map[string][]string{"sns": {"topic2"}, "sqs": {"queue2"}},
			wantVisibility:   "90",
		},
	}

	for _, tc := range tests {
		t.Run(tc.strategy, func(t *testing.T) {
			_, stderr, err := runLstk(t, ctx, "", e, "reset", "--force")
			require.NoError(t, err, "reset failed: %s", stderr)

			_, stderr, err = runLstk(t, ctx, "", e, "snapshot", "load", snap1)
			require.NoError(t, err, "loading snapshot1 failed: %s", stderr)

			_, stderr, err = runLstk(t, ctx, "", e, "snapshot", "load", snap2, "--merge="+tc.strategy)
			require.NoError(t, err, "loading snapshot2 (merge=%s) failed: %s", tc.strategy, stderr)

			assert.ElementsMatch(t, tc.wantUSEastSNS, snsTopicNames(t, ctx, e, "us-east-1"), "us-east-1 SNS topics")
			assert.ElementsMatch(t, tc.wantUSEastS3, s3BucketNames(t, ctx, e), "S3 buckets")
			assert.ElementsMatch(t, tc.wantAPSoutheast2["sns"], snsTopicNames(t, ctx, e, "ap-southeast-2"), "ap-southeast-2 SNS topics")

			usEastQueues := sqsQueueURLsByName(t, ctx, e, "us-east-1")
			gotUSEastSQS := make([]string, 0, len(usEastQueues))
			for name := range usEastQueues {
				gotUSEastSQS = append(gotUSEastSQS, name)
			}
			assert.ElementsMatch(t, tc.wantUSEastSQS, gotUSEastSQS, "us-east-1 SQS queues")

			apSoutheastQueues := sqsQueueURLsByName(t, ctx, e, "ap-southeast-2")
			gotAPSoutheastSQS := make([]string, 0, len(apSoutheastQueues))
			for name := range apSoutheastQueues {
				gotAPSoutheastSQS = append(gotAPSoutheastSQS, name)
			}
			assert.ElementsMatch(t, tc.wantAPSoutheast2["sqs"], gotAPSoutheastSQS, "ap-southeast-2 SQS queues")

			queueToReplaceURL, ok := usEastQueues["queue-to-replace"]
			require.True(t, ok, "queue-to-replace should exist in us-east-1 after merge=%s", tc.strategy)
			assert.Equal(t, tc.wantVisibility, sqsQueueVisibilityTimeout(t, ctx, e, "us-east-1", queueToReplaceURL),
				"queue-to-replace VisibilityTimeout after merge=%s", tc.strategy)
		})
	}
}
