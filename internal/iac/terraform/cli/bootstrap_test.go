package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeAWS installs a shell script at <dir>/aws that:
//   - exits 0 for `s3api head-bucket` / `dynamodb describe-table` only when
//     the resource name appears in an "existing" file the test can populate
//   - records every invocation into <dir>/aws.log
//   - exits 0 for create-bucket / create-table
func writeFakeAWS(t *testing.T, dir string) (logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake aws script not supported on Windows")
	}
	logPath = filepath.Join(dir, "aws.log")
	existsPath := filepath.Join(dir, "existing")
	script := `#!/bin/sh
echo "$@" >> ` + logPath + `
# Strip leading global flags so we can find the subcommand
while [ $# -gt 0 ]; do
  case "$1" in
    --endpoint-url|--region) shift 2 ;;
    *) break ;;
  esac
done
case "$1 $2" in
  "s3api head-bucket")
    grep -q -F "$4" ` + existsPath + ` 2>/dev/null && exit 0 || { echo "Not Found" >&2; exit 254; }
    ;;
  "dynamodb describe-table")
    grep -q -F "$4" ` + existsPath + ` 2>/dev/null && exit 0 || { echo "ResourceNotFoundException" >&2; exit 254; }
    ;;
  *)
    exit 0
    ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aws"), []byte(script), 0o755))
	require.NoError(t, os.WriteFile(existsPath, []byte(""), 0o644))
	return logPath
}

func withPATH(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", dir+":/bin:/usr/bin"))
	t.Cleanup(func() { _ = os.Setenv("PATH", orig) })
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

// baseOpts builds an Options with the default endpoint and a derived S3
// endpoint set, mirroring what cmd/terraform.go would produce.
func baseOpts(endpointURL, s3EndpointURL string) Options {
	return resolveOptionsWithDefaults(Options{
		Endpoints: []Endpoint{
			{Service: "", URL: endpointURL},
			{Service: "S3", URL: s3EndpointURL},
		},
	})
}

func TestEnsureBackendResourcesCreatesMissingBucketAndTable(t *testing.T) {
	dir := t.TempDir()
	logPath := writeFakeAWS(t, dir)
	withPATH(t, dir)

	var stderr bytes.Buffer
	err := ensureBackendResources(
		context.Background(),
		baseOpts("http://localhost:4566", "http://s3.localhost.localstack.cloud:4566"),
		S3BackendConfig{Bucket: "tf-state", DynamoDBTable: "tf-lock", Region: "us-east-1"},
		&stderr,
	)
	require.NoError(t, err)
	assert.Empty(t, stderr.String(), "no warnings expected")

	log := readLog(t, logPath)
	assert.Contains(t, log, "s3api head-bucket --bucket tf-state")
	assert.Contains(t, log, "s3api create-bucket --bucket tf-state")
	assert.Contains(t, log, "dynamodb describe-table --table-name tf-lock")
	assert.Contains(t, log, "dynamodb create-table --table-name tf-lock")
	assert.Contains(t, log, "--billing-mode PAY_PER_REQUEST")
}

func TestEnsureBackendResourcesSkipsCreateWhenBucketExists(t *testing.T) {
	dir := t.TempDir()
	logPath := writeFakeAWS(t, dir)
	// mark the bucket and table as already existing
	require.NoError(t, os.WriteFile(filepath.Join(dir, "existing"), []byte("tf-state\ntf-lock\n"), 0o644))
	withPATH(t, dir)

	var stderr bytes.Buffer
	err := ensureBackendResources(
		context.Background(),
		baseOpts("http://localhost:4566", "http://s3.localhost.localstack.cloud:4566"),
		S3BackendConfig{Bucket: "tf-state", DynamoDBTable: "tf-lock", Region: "us-east-1"},
		&stderr,
	)
	require.NoError(t, err)
	assert.Empty(t, stderr.String())

	log := readLog(t, logPath)
	assert.Contains(t, log, "head-bucket")
	assert.Contains(t, log, "describe-table")
	assert.NotContains(t, log, "create-bucket")
	assert.NotContains(t, log, "create-table")
}

func TestEnsureBackendResourcesPassesLocationConstraintForNonDefaultRegion(t *testing.T) {
	dir := t.TempDir()
	logPath := writeFakeAWS(t, dir)
	withPATH(t, dir)

	var stderr bytes.Buffer
	err := ensureBackendResources(
		context.Background(),
		baseOpts("http://localhost:4566", "http://s3.localhost.localstack.cloud:4566"),
		S3BackendConfig{Bucket: "tf-state", Region: "eu-west-1"},
		&stderr,
	)
	require.NoError(t, err)
	log := readLog(t, logPath)
	assert.Contains(t, log, "--create-bucket-configuration LocationConstraint=eu-west-1")
}

func TestEnsureBackendResourcesUsesPerServiceEndpoints(t *testing.T) {
	dir := t.TempDir()
	logPath := writeFakeAWS(t, dir)
	withPATH(t, dir)

	opts := resolveOptionsWithDefaults(Options{
		Endpoints: []Endpoint{
			{Service: "", URL: "http://default:4566"},
			{Service: "S3", URL: "http://s3-custom:9000"},
			{Service: "DYNAMODB", URL: "http://ddb-custom:9000"},
		},
	})

	var stderr bytes.Buffer
	err := ensureBackendResources(
		context.Background(),
		opts,
		S3BackendConfig{Bucket: "b", DynamoDBTable: "t", Region: "us-east-1"},
		&stderr,
	)
	require.NoError(t, err)
	log := readLog(t, logPath)
	assert.Contains(t, log, "--endpoint-url http://s3-custom:9000")
	assert.Contains(t, log, "--endpoint-url http://ddb-custom:9000")
}

func TestEnsureBackendResourcesFallsBackToDefaultEndpointForDynamoDB(t *testing.T) {
	// No DYNAMODB-specific endpoint configured: bootstrap should use the
	// default endpoint for the dynamodb sub-commands.
	dir := t.TempDir()
	logPath := writeFakeAWS(t, dir)
	withPATH(t, dir)

	var stderr bytes.Buffer
	err := ensureBackendResources(
		context.Background(),
		baseOpts("http://default:4566", "http://s3.default:4566"),
		S3BackendConfig{Bucket: "b", DynamoDBTable: "t", Region: "us-east-1"},
		&stderr,
	)
	require.NoError(t, err)
	log := readLog(t, logPath)
	assert.Contains(t, log, "--endpoint-url http://s3.default:4566 --region us-east-1 s3api")
	assert.Contains(t, log, "--endpoint-url http://default:4566 --region us-east-1 dynamodb")
}

func TestEnsureBackendResourcesReturnsErrorWhenAWSMissing(t *testing.T) {
	// point PATH at an empty directory so `aws` cannot be found
	withPATH(t, t.TempDir())
	var stderr bytes.Buffer
	err := ensureBackendResources(
		context.Background(),
		baseOpts("http://localhost:4566", "http://s3.localhost.localstack.cloud:4566"),
		S3BackendConfig{Bucket: "b"},
		&stderr,
	)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "aws CLI not found"), "got: %v", err)
}

func TestEnsureBackendResourcesWarnsOnCreateFailure(t *testing.T) {
	dir := t.TempDir()
	// fake aws that fails create-bucket
	script := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    --endpoint-url|--region) shift 2 ;;
    *) break ;;
  esac
done
if [ "$1 $2" = "s3api head-bucket" ]; then
  echo "Not Found" >&2
  exit 254
fi
if [ "$1 $2" = "s3api create-bucket" ]; then
  echo "AccessDenied: denied" >&2
  exit 1
fi
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aws"), []byte(script), 0o755))
	withPATH(t, dir)

	var stderr bytes.Buffer
	err := ensureBackendResources(
		context.Background(),
		baseOpts("http://localhost:4566", "http://s3.localhost.localstack.cloud:4566"),
		S3BackendConfig{Bucket: "b", Region: "us-east-1"},
		&stderr,
	)
	require.NoError(t, err) // non-fatal
	assert.Contains(t, stderr.String(), "warning: ensuring S3 state bucket")
	assert.Contains(t, stderr.String(), "AccessDenied")
}
