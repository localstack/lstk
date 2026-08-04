package snapshot_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/localstack/lstk/internal/api"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// assertFeatureUnavailable checks the shared "requires a paid plan" rendering:
// a silent error (so the top-level handler doesn't re-print it) plus an
// ErrorEvent carrying the pricing CTA.
func assertFeatureUnavailable(t *testing.T, err error, events []output.Event) {
	t.Helper()
	require.Error(t, err)
	require.ErrorIs(t, err, snapshot.ErrSnapshotFeatureUnavailable)
	assert.True(t, output.IsSilent(err), "the feature-unavailable error should be silent so it isn't double-rendered")

	var errEvent *output.ErrorEvent
	for _, e := range events {
		if ev, ok := e.(output.ErrorEvent); ok {
			errEvent = &ev
		}
	}
	require.NotNil(t, errEvent, "a structured ErrorEvent should have been emitted")
	assert.Equal(t, "Snapshots require a paid LocalStack plan", errEvent.Title)
	assert.NotContains(t, errEvent.Title, "404", "the raw HTTP status must never reach the user")

	var values []string
	for _, a := range errEvent.Actions {
		values = append(values, a.Value)
	}
	assert.Contains(t, values, "https://www.localstack.cloud/pricing")
}

func TestLoadLocal_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	src := writeSnapshotFile(t, "ZIP_DATA")
	// Wrapped, mirroring how SaveLocal/LoadRemoteS3 wrap client errors with %w.
	client := mockLocalClientReturning(t, fmt.Errorf("import: %w", snapshot.ErrSnapshotFeatureUnavailable))
	sink, getEvents := captureEvents(t)

	err := snapshot.LoadLocal(context.Background(), healthyRunningMock(t), awsContainers, client, "", src, "", nopStarter, sink)
	assertFeatureUnavailable(t, err, getEvents())
}

func TestLoadPod_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	loader := NewMockPodLoader(ctrl)
	loader.EXPECT().LoadPodSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, snapshot.ErrSnapshotFeatureUnavailable)
	sink, getEvents := captureEvents(t)

	err := snapshot.LoadPod(context.Background(), healthyRunningMock(t), awsContainers, loader, "", "my-baseline", 0, "test-token", "", nopStarter, sink)
	assertFeatureUnavailable(t, err, getEvents())
}

// save() translated no sentinels before this change, so this covers a brand-new
// branch rather than an extra case on an existing one.
func TestSaveLocal_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	exporter := mockExporterReturningError(t, snapshot.ErrSnapshotFeatureUnavailable)
	sink, getEvents := captureEvents(t)

	dest := writeSnapshotFile(t, "")
	err := snapshot.SaveLocal(context.Background(), healthyRunningMock(t), awsContainers, exporter, "", dest, nil, sink)
	assertFeatureUnavailable(t, err, getEvents())
}

func TestSavePod_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	saver := NewMockPodSaver(ctrl)
	saver.EXPECT().SavePodSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(snapshot.PodSaveResult{}, snapshot.ErrSnapshotFeatureUnavailable)
	sink, getEvents := captureEvents(t)

	err := snapshot.SavePod(context.Background(), healthyRunningMock(t), awsContainers, saver, "", "my-baseline", "test-token", nil, sink)
	assertFeatureUnavailable(t, err, getEvents())
}

func TestDiffPod_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	differ := NewMockPodDiffer(ctrl)
	differ.EXPECT().DiffPodSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, snapshot.ErrSnapshotFeatureUnavailable)
	sink, getEvents := captureEvents(t)

	err := snapshot.DiffPod(context.Background(), healthyRunningMock(t), awsContainers, differ, "", "my-baseline", 0, "test-token", "", sink)
	assertFeatureUnavailable(t, err, getEvents())
}

func TestRemove_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	remover := NewMockPodRemover(ctrl)
	remover.EXPECT().RemovePodSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(snapshot.ErrSnapshotFeatureUnavailable)
	sink, getEvents := captureEvents(t)

	err := snapshot.Remove(context.Background(), healthyRunningMock(t), awsContainers, "my-baseline", "test-token", remover, "", true, sink)
	assertFeatureUnavailable(t, err, getEvents())
}

func TestListRemoteS3_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	client := NewMockRemoteClient(ctrl)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	client.EXPECT().S3BucketExists(gomock.Any(), "bucket").Return(true, nil)
	client.EXPECT().RegisterRemote(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	client.EXPECT().ListPodsRemote(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), "").
		Return(nil, snapshot.ErrSnapshotFeatureUnavailable)
	sink, getEvents := captureEvents(t)

	err := snapshot.ListRemoteS3(context.Background(), mockRT, awsContainers, client, "", "s3://bucket",
		snapshot.S3Credentials{AccessKeyID: "a", SecretAccessKey: "b"}, "", sink)
	assertFeatureUnavailable(t, err, getEvents())
}

// The S3 remote paths register the remote before the pod call, so an unentitled
// emulator fails at RegisterRemote — which is wrapped with %w and must still be
// recognised.
func TestSaveRemoteS3_FeatureUnavailableOnRegisterRemote(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	client := NewMockRemoteClient(ctrl)
	client.EXPECT().S3BucketExists(gomock.Any(), "bucket").Return(true, nil)
	client.EXPECT().RegisterRemote(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(snapshot.ErrSnapshotFeatureUnavailable)
	sink, getEvents := captureEvents(t)

	err := snapshot.SaveRemoteS3(context.Background(), healthyRunningMock(t), awsContainers, client, "", "my-pod", "s3://bucket",
		snapshot.S3Credentials{AccessKeyID: "a", SecretAccessKey: "b"}, "", nil, sink)
	assertFeatureUnavailable(t, err, getEvents())
}

// stubLister/stubInspector stand in for the platform client on the list/show
// paths, which take a plain interface rather than a generated mock.
type stubLister struct{ err error }

func (s stubLister) ListCloudPods(context.Context, string, string) ([]api.CloudPod, error) {
	return nil, s.err
}

type stubInspector struct{ err error }

func (s stubInspector) GetCloudPod(context.Context, string, string, int) (*api.CloudPodDetails, error) {
	return nil, s.err
}

type stubVersionLister struct{ err error }

func (s stubVersionLister) GetCloudPodVersions(context.Context, string, string) ([]api.CloudPodVersion, error) {
	return nil, s.err
}

// list/show query the platform, which reports a plan without Cloud Pods as a
// 403 rather than the emulator's empty 404 — same message, different signal.
func TestList_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	sink, getEvents := captureEvents(t)

	err := snapshot.List(context.Background(), stubLister{err: api.ErrCloudPodsForbidden}, "test-token", "me", sink)
	assertFeatureUnavailable(t, err, getEvents())
}

func TestShow_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	sink, getEvents := captureEvents(t)

	err := snapshot.Show(context.Background(), stubInspector{err: api.ErrCloudPodsForbidden}, "test-token", "my-baseline", 0, sink)
	assertFeatureUnavailable(t, err, getEvents())
}

// versions is the third platform-API command, so it needs the same 403 handling
// as list/show — it was added before that handling existed.
func TestVersions_FeatureUnavailable(t *testing.T) {
	t.Parallel()
	sink, getEvents := captureEvents(t)

	err := snapshot.Versions(context.Background(), stubVersionLister{err: api.ErrCloudPodsForbidden}, "test-token", "my-baseline", sink)
	assertFeatureUnavailable(t, err, getEvents())
}
