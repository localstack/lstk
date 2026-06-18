package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/localstack/lstk/internal/awscli"
	"github.com/localstack/lstk/internal/log"
)

// awsRunner runs a single `aws` CLI invocation against LocalStack and returns
// its combined output (stdout+stderr). It is the seam provisioning is unit-tested
// through: tests inject a fake, production uses newAWSRunner.
type awsRunner func(ctx context.Context, args ...string) (output string, err error)

// provisionBackend ensures the S3 backend's resources exist in LocalStack before
// `terraform init` configures the backend: it creates the state bucket if
// absent and, when the backend configures DynamoDB-based locking
// (`dynamodb_table`), the lock table. A fresh LocalStack has neither, so init
// would otherwise fail.
//
// Rather than linking the AWS SDK, provisioning shells out to the `aws` CLI
// (the same tool `lstk aws` proxies), targeting the resolved LocalStack endpoint
// with forced mock credentials. It is idempotent. Returns awscli.ErrNotInstalled
// if the `aws` binary is missing so the caller can surface an install hint.
func provisionBackend(ctx context.Context, backend *s3Backend, e endpointForm, logger log.Logger) error {
	if backend.bucket == "" {
		// Terraform requires `bucket`; if it is missing or non-literal we cannot
		// provision. Let terraform surface the configuration error itself.
		logger.Info("terraform: backend \"s3\" has no literal bucket; skipping bucket provisioning")
		return nil
	}

	if err := awscli.CheckInstalled(); err != nil {
		return err
	}

	region := backend.region
	if region == "" {
		region = e.region
	}

	run := newAWSRunner(e.endpointURL, region)
	if err := ensureBucket(ctx, run, backend.bucket, region, logger); err != nil {
		return err
	}

	if backend.dynamoDBTable == "" {
		// S3-native locking (use_lockfile) or no locking — no table to create.
		return nil
	}
	return ensureLockTable(ctx, run, backend.dynamoDBTable, logger)
}

// newAWSRunner builds an awsRunner that invokes the `aws` CLI against the given
// LocalStack endpoint. Credentials are forced to the mock values (matching the
// rest of the terraform proxy) via awscli.BuildEnv, and S3 path-style addressing
// is forced so the bare endpoint is used verbatim (no virtual-host DNS needed).
func newAWSRunner(endpointURL, region string) awsRunner {
	return func(ctx context.Context, args ...string) (string, error) {
		full := make([]string, 0, len(args)+4)
		full = append(full, "--endpoint-url", endpointURL)
		if region != "" {
			full = append(full, "--region", region)
		}
		full = append(full, args...)

		cmd := exec.CommandContext(ctx, "aws", full...)
		cmd.Env = append(awscli.BuildEnv(os.Environ()), "AWS_S3_ADDRESSING_STYLE=path")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		return out.String(), err
	}
}

// ensureBucket creates the state bucket if it does not already exist. An
// already-existing/owned bucket is treated as success. A LocationConstraint is
// supplied only for non-us-east-1 regions (us-east-1 rejects an explicit
// constraint).
func ensureBucket(ctx context.Context, run awsRunner, bucket, region string, logger log.Logger) error {
	if _, err := run(ctx, "s3api", "head-bucket", "--bucket", bucket); err == nil {
		logger.Info("terraform: state bucket %q already exists in LocalStack", bucket)
		return nil
	}

	args := []string{"s3api", "create-bucket", "--bucket", bucket}
	if needsLocationConstraint(region) {
		args = append(args, "--create-bucket-configuration", "LocationConstraint="+region)
	}
	if out, err := run(ctx, args...); err != nil {
		if bucketAlreadyOwned(out) {
			logger.Info("terraform: state bucket %q already owned in LocalStack", bucket)
			return nil
		}
		return fmt.Errorf("creating state bucket %q in LocalStack: %w: %s", bucket, err, strings.TrimSpace(out))
	}
	logger.Info("terraform: created state bucket %q in LocalStack", bucket)
	return nil
}

// needsLocationConstraint reports whether a bucket in region must carry an
// explicit LocationConstraint. us-east-1 (and empty, which defaults to it) must
// not — S3 rejects an explicit constraint there.
func needsLocationConstraint(region string) bool {
	return region != "" && region != "us-east-1"
}

// bucketAlreadyOwned reports whether a create-bucket failure means the bucket
// already exists under our ownership — an idempotent success, not a failure.
func bucketAlreadyOwned(output string) bool {
	return strings.Contains(output, "BucketAlreadyOwnedByYou") ||
		strings.Contains(output, "BucketAlreadyExists")
}

// ensureLockTable creates the DynamoDB lock table with the schema Terraform's
// S3 backend expects (hash key LockID, type S) if it does not already exist.
// Idempotent: an existing table is treated as success.
func ensureLockTable(ctx context.Context, run awsRunner, table string, logger log.Logger) error {
	if _, err := run(ctx, "dynamodb", "describe-table", "--table-name", table); err == nil {
		logger.Info("terraform: lock table %q already exists in LocalStack", table)
		return nil
	}

	out, err := run(ctx, "dynamodb", "create-table",
		"--table-name", table,
		"--attribute-definitions", "AttributeName=LockID,AttributeType=S",
		"--key-schema", "AttributeName=LockID,KeyType=HASH",
		"--billing-mode", "PAY_PER_REQUEST",
	)
	if err != nil {
		if strings.Contains(out, "ResourceInUseException") {
			logger.Info("terraform: lock table %q already exists in LocalStack", table)
			return nil
		}
		return fmt.Errorf("creating lock table %q in LocalStack: %w: %s", table, err, strings.TrimSpace(out))
	}
	logger.Info("terraform: created lock table %q in LocalStack", table)
	return nil
}
