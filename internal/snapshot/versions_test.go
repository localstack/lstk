package snapshot_test

import (
	"context"
	"fmt"
	"io"
	"strings"
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
	assert.Equal(t, []string{"Version", "Created", "LocalStack", "Services"}, table.Headers)
	require.Len(t, table.Rows, 2)
	assert.Equal(t, []string{"2", "2026-07-01 12:00 UTC", "2026.06", "s3, lambda"}, table.Rows[0])
	assert.Equal(t, []string{"1", "-", "-", "-"}, table.Rows[1], "missing values render as a dash")
}

// TestVersions_ServicesNotTruncated: the services cell is emitted in full and is
// the table's widest column, so formatTableWidth is the only thing that shortens
// it — which means a wide terminal shows every name and a redirected stdout shows
// them all untruncated. Capping in the domain layer would defeat both.
func TestVersions_ServicesNotTruncated(t *testing.T) {
	t.Parallel()
	services := []string{"s3", "sqs", "sns", "iam", "ec2", "rds", "lambda", "dynamodb", "cloudformation", "apigateway"}
	lister := &fakeVersionLister{versions: []api.CloudPodVersion{{Version: 1, Services: services}}}

	sink, getEvents := captureEvents(t)
	require.NoError(t, snapshot.Versions(context.Background(), lister, "tok", "p", sink))

	table := firstTable(t, getEvents())
	require.Len(t, table.Rows, 1)
	assert.Equal(t, strings.Join(services, ", "), table.Rows[0][3])
	assert.NotContains(t, table.Rows[0][3], "more", "no domain-side summarisation")
	assert.NotContains(t, table.Rows[0][3], "…", "no domain-side ellipsis")
}

// TestVersions_DescriptionIsNotAColumn pins the deliberate omission: nothing in
// lstk sets the platform's description field, so a column for it rendered "-" on
// every row. The space goes to service names instead.
func TestVersions_DescriptionIsNotAColumn(t *testing.T) {
	t.Parallel()
	lister := &fakeVersionLister{versions: []api.CloudPodVersion{
		{Version: 1, Services: []string{"s3"}, Description: "should-not-render"},
	}}

	sink, getEvents := captureEvents(t)
	require.NoError(t, snapshot.Versions(context.Background(), lister, "tok", "p", sink))

	table := firstTable(t, getEvents())
	assert.NotContains(t, table.Headers, "Description")
	assert.NotContains(t, table.Rows[0], "should-not-render")
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
