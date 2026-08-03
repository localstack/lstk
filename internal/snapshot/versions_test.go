package snapshot_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeVersionLister stands in for api.PlatformClient. The interface has a single
// method, so a hand-written fake is clearer here than a generated mock (Show,
// its sibling, has no mock either).
type fakeVersionLister struct {
	versions []api.CloudPodVersion
	err      error
	gotToken string
	gotPod   string
}

func (f *fakeVersionLister) GetCloudPodVersions(_ context.Context, authToken, podName string) ([]api.CloudPodVersion, error) {
	f.gotToken, f.gotPod = authToken, podName
	return f.versions, f.err
}

func firstTable(t *testing.T, events []output.Event) output.TableEvent {
	t.Helper()
	for _, e := range events {
		deferred, ok := e.(output.DeferredEvent)
		if !ok {
			continue
		}
		if table, ok := deferred.Inner.(output.TableEvent); ok {
			return table
		}
	}
	t.Fatal("no TableEvent was emitted")
	return output.TableEvent{}
}

func TestVersions_RendersTable(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	lister := &fakeVersionLister{versions: []api.CloudPodVersion{
		{Version: 2, Created: &created, LocalStackVersion: "2026.06", Services: []string{"s3", "lambda"}, Description: "nightly"},
		{Version: 1},
	}}

	sink, getEvents := captureEvents(t)
	require.NoError(t, snapshot.Versions(context.Background(), lister, "test-token", "my-baseline", sink))

	assert.Equal(t, "test-token", lister.gotToken)
	assert.Equal(t, "my-baseline", lister.gotPod)

	table := firstTable(t, getEvents())
	assert.Equal(t, []string{"Version", "Created", "LocalStack", "Services", "Description"}, table.Headers)
	require.Len(t, table.Rows, 2)
	assert.Equal(t, []string{"2", "2026-07-01 12:00 UTC", "2026.06", "s3, lambda", "nightly"}, table.Rows[0])
	assert.Equal(t, []string{"1", "-", "-", "-", "-"}, table.Rows[1], "missing values render as a dash")
}

func TestVersions_CountLinePluralization(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		count int
		want  string
	}{
		{1, "~ 1 version\n"},
		{2, "~ 2 versions\n"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			versions := make([]api.CloudPodVersion, tc.count)
			for i := range versions {
				versions[i] = api.CloudPodVersion{Version: tc.count - i}
			}

			sink, getEvents := captureEvents(t)
			require.NoError(t, snapshot.Versions(context.Background(), &fakeVersionLister{versions: versions}, "tok", "p", sink))

			var got string
			for _, e := range getEvents() {
				if deferred, ok := e.(output.DeferredEvent); ok {
					if msg, ok := deferred.Inner.(output.MessageEvent); ok && msg.Severity == output.SeveritySecondary {
						got = msg.Text
					}
				}
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestVersions_NoVersions(t *testing.T) {
	t.Parallel()
	sink, getEvents := captureEvents(t)
	require.NoError(t, snapshot.Versions(context.Background(), &fakeVersionLister{}, "tok", "empty", sink))

	var note string
	for _, e := range getEvents() {
		if deferred, ok := e.(output.DeferredEvent); ok {
			if msg, ok := deferred.Inner.(output.MessageEvent); ok && msg.Severity == output.SeverityNote {
				note = msg.Text
			}
		}
		if _, ok := e.(output.TableEvent); ok {
			t.Error("no table should be emitted when there are no versions")
		}
	}
	assert.Contains(t, note, "No versions found for 'pod:empty'")
}

func TestVersions_RequiresAuthToken(t *testing.T) {
	t.Parallel()
	lister := &fakeVersionLister{}
	sink, getEvents := captureEvents(t)

	err := snapshot.Versions(context.Background(), lister, "", "my-baseline", sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
	assert.Empty(t, lister.gotPod, "the platform must not be called without a token")

	var gotErrorEvent bool
	for _, e := range getEvents() {
		if ev, ok := e.(output.ErrorEvent); ok {
			gotErrorEvent = true
			assert.Contains(t, ev.Title, "Authentication required")
		}
	}
	assert.True(t, gotErrorEvent, "ErrorEvent should have been emitted")
}

func TestVersions_PodNotFound(t *testing.T) {
	t.Parallel()
	lister := &fakeVersionLister{err: api.ErrCloudPodNotFound}
	sink, getEvents := captureEvents(t)

	err := snapshot.Versions(context.Background(), lister, "tok", "missing", sink)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))

	var gotErrorEvent bool
	for _, e := range getEvents() {
		if ev, ok := e.(output.ErrorEvent); ok {
			gotErrorEvent = true
			assert.Contains(t, ev.Title, "'pod:missing' not found")
			require.NotEmpty(t, ev.Actions)
			assert.Equal(t, "lstk snapshot list", ev.Actions[0].Value)
		}
	}
	assert.True(t, gotErrorEvent, "ErrorEvent should have been emitted")
}

func TestVersions_ListerError(t *testing.T) {
	t.Parallel()
	sink := output.NewPlainSink(io.Discard)
	err := snapshot.Versions(context.Background(), &fakeVersionLister{err: fmt.Errorf("platform unreachable")}, "tok", "p", sink)
	require.Error(t, err)
	assert.False(t, output.IsSilent(err), "an unexpected transport error should fall through to the generic handler")
	assert.Contains(t, err.Error(), "platform unreachable")
}

func TestTruncateServices(t *testing.T) {
	t.Parallel()

	t.Run("fits within the budget", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", snapshot.TruncateServices(nil, 20))
		assert.Equal(t, "s3", snapshot.TruncateServices([]string{"s3"}, 20))
		assert.Equal(t, "s3, sqs, sns", snapshot.TruncateServices([]string{"s3", "sqs", "sns"}, 20))
	})

	t.Run("summarises the overflow", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "s3, sqs, sns +3 more",
			snapshot.TruncateServices([]string{"s3", "sqs", "sns", "iam", "ec2", "rds"}, 20))
	})

	t.Run("always shows at least one name", func(t *testing.T) {
		t.Parallel()
		got := snapshot.TruncateServices([]string{"an-extremely-long-service-name", "s3"}, 20)
		assert.Equal(t, "an-extremel… +1 more", got)
		assert.Len(t, []rune(got), 20, "the cell should fill, but not exceed, the budget")
	})

	// The whole point of the budget is that the rendered cell stays bounded, so
	// assert that directly across a range of shapes and budgets. This is a hard
	// bound: the "+N more" suffix and the ellipsis both count against it.
	t.Run("never exceeds the budget", func(t *testing.T) {
		t.Parallel()
		inputs := [][]string{
			{"s3"},
			{"s3", "sqs"},
			{"s3", "sqs", "sns", "iam", "ec2", "rds", "lambda", "dynamodb"},
			{"a-really-quite-long-service-name", "another-long-one", "s3"},
			{"αβγδεζηθικλμνξοπρστυφχψω", "s3"},
		}
		for _, max := range []int{1, 3, 8, 15, 20, 30} {
			for _, services := range inputs {
				got := snapshot.TruncateServices(services, max)
				assert.LessOrEqual(t, len([]rune(got)), max,
					"budget %d exceeded for %v: %q", max, services, got)
			}
		}
	})
}

// TestTruncateDescription: max is a hard bound on the rendered width, so the
// ellipsis marking the cut counts against it.
func TestTruncateDescription(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", snapshot.TruncateDescription("", 10))
	assert.Equal(t, "short", snapshot.TruncateDescription("short", 10))
	assert.Equal(t, "exactly-10", snapshot.TruncateDescription("exactly-10", 10))
	assert.Equal(t, "012345678…", snapshot.TruncateDescription("0123456789abc", 10))
	assert.Equal(t, "trailing…", snapshot.TruncateDescription("trailing  space", 10), "the cut should not leave trailing spaces")
	// Multi-byte input must be cut on rune boundaries, never mid-character.
	assert.Equal(t, "αααα…", snapshot.TruncateDescription("αααααααα", 5))
	// Degenerate budgets still produce something bounded rather than panicking.
	assert.Equal(t, "…", snapshot.TruncateDescription("abcdef", 1))
	assert.Equal(t, "", snapshot.TruncateDescription("abcdef", 0))

	for _, max := range []int{0, 1, 2, 5, 40} {
		got := snapshot.TruncateDescription("a description long enough to need cutting at every budget", max)
		assert.LessOrEqual(t, len([]rune(got)), max, "budget %d exceeded: %q", max, got)
	}
}
