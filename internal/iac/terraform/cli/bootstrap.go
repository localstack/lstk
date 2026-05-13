package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ensureBackendResources creates the S3 state bucket and DynamoDB lock table
// on LocalStack if they don't already exist, by shelling out to the `aws` CLI.
// Errors are surfaced to the caller; the caller decides whether they are
// fatal. The expected case is that terraform's own backend bootstrap would
// fail without a pre-existing bucket — this just removes that friction.
func ensureBackendResources(ctx context.Context, r Options, b S3BackendConfig, stderr io.Writer) error {
	awsBin, err := exec.LookPath("aws")
	if err != nil {
		return fmt.Errorf("aws CLI not found in PATH (required to bootstrap the terraform state backend on LocalStack)")
	}

	region := b.Region
	if region == "" {
		region = r.Region
	}
	env := buildEnv(nil, r) // inject test/test credentials when not set

	if b.Bucket != "" {
		s3Endpoint := r.endpointFor("S3")
		if err := ensureBucketViaCLI(ctx, awsBin, env, s3Endpoint, region, b.Bucket); err != nil {
			_, _ = fmt.Fprintf(stderr, "lstk: warning: ensuring S3 state bucket %q failed: %v\n", b.Bucket, err)
		}
	}
	if b.DynamoDBTable != "" {
		ddbEndpoint := r.endpointFor("DYNAMODB")
		if err := ensureLockTableViaCLI(ctx, awsBin, env, ddbEndpoint, region, b.DynamoDBTable); err != nil {
			_, _ = fmt.Fprintf(stderr, "lstk: warning: ensuring DynamoDB lock table %q failed: %v\n", b.DynamoDBTable, err)
		}
	}
	return nil
}

func ensureBucketViaCLI(ctx context.Context, awsBin string, env []string, endpoint, region, bucket string) error {
	headArgs := []string{
		"--endpoint-url", endpoint,
		"--region", region,
		"s3api", "head-bucket",
		"--bucket", bucket,
	}
	if err := runAWS(ctx, awsBin, env, headArgs); err == nil {
		return nil
	}

	createArgs := []string{
		"--endpoint-url", endpoint,
		"--region", region,
		"s3api", "create-bucket",
		"--bucket", bucket,
	}
	if region != "us-east-1" {
		createArgs = append(createArgs, "--create-bucket-configuration", "LocationConstraint="+region)
	}
	return runAWS(ctx, awsBin, env, createArgs)
}

func ensureLockTableViaCLI(ctx context.Context, awsBin string, env []string, endpoint, region, table string) error {
	describeArgs := []string{
		"--endpoint-url", endpoint,
		"--region", region,
		"dynamodb", "describe-table",
		"--table-name", table,
	}
	if err := runAWS(ctx, awsBin, env, describeArgs); err == nil {
		return nil
	}

	createArgs := []string{
		"--endpoint-url", endpoint,
		"--region", region,
		"dynamodb", "create-table",
		"--table-name", table,
		"--billing-mode", "PAY_PER_REQUEST",
		"--key-schema", "AttributeName=LockID,KeyType=HASH",
		"--attribute-definitions", "AttributeName=LockID,AttributeType=S",
	}
	return runAWS(ctx, awsBin, env, createArgs)
}

func runAWS(ctx context.Context, bin string, env, args []string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && msg != "" {
			return fmt.Errorf("aws %s: %s", strings.Join(args, " "), msg)
		}
		return err
	}
	return nil
}
