package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/internal/log"
)

func TestNeedsLocationConstraint(t *testing.T) {
	assert.False(t, needsLocationConstraint("us-east-1"), "us-east-1 must not carry a constraint")
	assert.False(t, needsLocationConstraint(""), "empty region must not carry a constraint")
	assert.True(t, needsLocationConstraint("eu-west-1"))
}

// recordingRunner captures the args of each aws invocation and returns canned
// results keyed by the verb (the second arg, e.g. "create-bucket").
type recordingRunner struct {
	calls   [][]string
	results map[string]struct {
		out string
		err error
	}
}

func (r *recordingRunner) run(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, args)
	if res, ok := r.results[args[1]]; ok {
		return res.out, res.err
	}
	return "", nil
}

func (r *recordingRunner) called(verb string) []string {
	for _, c := range r.calls {
		if c[1] == verb {
			return c
		}
	}
	return nil
}

func newRunner() *recordingRunner {
	return &recordingRunner{results: map[string]struct {
		out string
		err error
	}{}}
}

func (r *recordingRunner) fail(verb, out string) {
	r.results[verb] = struct {
		out string
		err error
	}{out: out, err: errors.New("exit status 254")}
}

func TestEnsureBucketCreatesWhenAbsent(t *testing.T) {
	r := newRunner()
	r.fail("head-bucket", "Not Found")
	require.NoError(t, ensureBucket(context.Background(), r.run, "b", "us-east-1", log.Nop()))

	create := r.called("create-bucket")
	require.NotNil(t, create, "create-bucket should be called")
	assert.NotContains(t, strings.Join(create, " "), "LocationConstraint", "us-east-1 → no LocationConstraint")
}

func TestEnsureBucketSetsConstraintForNonDefaultRegion(t *testing.T) {
	r := newRunner()
	r.fail("head-bucket", "Not Found")
	require.NoError(t, ensureBucket(context.Background(), r.run, "b", "eu-west-1", log.Nop()))

	create := r.called("create-bucket")
	require.NotNil(t, create)
	assert.Contains(t, strings.Join(create, " "), "LocationConstraint=eu-west-1")
}

func TestEnsureBucketSkipsWhenPresent(t *testing.T) {
	r := newRunner() // head-bucket succeeds → already exists
	require.NoError(t, ensureBucket(context.Background(), r.run, "b", "us-east-1", log.Nop()))
	assert.Nil(t, r.called("create-bucket"), "existing bucket → no create")
}

func TestEnsureBucketTreatsAlreadyOwnedAsSuccess(t *testing.T) {
	r := newRunner()
	r.fail("head-bucket", "Not Found")
	r.fail("create-bucket", "An error occurred (BucketAlreadyOwnedByYou) when calling the CreateBucket operation")
	require.NoError(t, ensureBucket(context.Background(), r.run, "b", "us-east-1", log.Nop()))
}

func TestEnsureBucketReturnsRealError(t *testing.T) {
	r := newRunner()
	r.fail("head-bucket", "Not Found")
	r.fail("create-bucket", "An error occurred (AccessDenied) when calling the CreateBucket operation")
	err := ensureBucket(context.Background(), r.run, "b", "us-east-1", log.Nop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AccessDenied")
}

func TestEnsureLockTableCreatesWhenAbsent(t *testing.T) {
	r := newRunner()
	r.fail("describe-table", "ResourceNotFoundException")
	require.NoError(t, ensureLockTable(context.Background(), r.run, "locks", log.Nop()))
	assert.NotNil(t, r.called("create-table"))
}

func TestEnsureLockTableSkipsWhenPresent(t *testing.T) {
	r := newRunner() // describe-table succeeds → already exists
	require.NoError(t, ensureLockTable(context.Background(), r.run, "locks", log.Nop()))
	assert.Nil(t, r.called("create-table"))
}

func TestEnsureLockTableTreatsInUseAsSuccess(t *testing.T) {
	r := newRunner()
	r.fail("describe-table", "ResourceNotFoundException")
	r.fail("create-table", "An error occurred (ResourceInUseException) when calling the CreateTable operation")
	require.NoError(t, ensureLockTable(context.Background(), r.run, "locks", log.Nop()))
}
