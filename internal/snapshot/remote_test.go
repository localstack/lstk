package snapshot_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestTemplatedRemoteURL(t *testing.T) {
	t.Parallel()

	// Placeholders must stay as literal {tokens} (not percent-encoded) so the
	// backend's str.format rendering recognizes them.
	got := snapshot.TemplatedRemoteURL("s3://my-bucket/prefix", false)
	assert.Equal(t, "s3://my-bucket/prefix?access_key_id={access_key_id}&secret_access_key={secret_access_key}", got)

	withToken := snapshot.TemplatedRemoteURL("s3://my-bucket/prefix", true)
	assert.Contains(t, withToken, "session_token={session_token}")

	// An existing query is preserved with an & separator.
	withQuery := snapshot.TemplatedRemoteURL("s3://my-bucket/prefix?region=eu-west-1", false)
	assert.True(t, strings.HasPrefix(withQuery, "s3://my-bucket/prefix?region=eu-west-1&access_key_id="))
}

func TestRemoteName_DeterministicAndDistinct(t *testing.T) {
	t.Parallel()
	a := snapshot.RemoteName("s3://bucket/one")
	b := snapshot.RemoteName("s3://bucket/one")
	c := snapshot.RemoteName("s3://bucket/two")
	assert.Equal(t, a, b, "same URL yields the same remote name")
	assert.NotEqual(t, a, c, "different URLs yield different remote names")
	assert.True(t, strings.HasPrefix(a, "lstk-s3-"))
}

func TestSaveRemoteS3_RegistersAndSaves(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	client := NewMockRemoteClient(ctrl)

	const s3URL = "s3://my-bucket/prefix"
	wantName := snapshot.RemoteName(s3URL)

	var gotURL string
	var gotParams map[string]string
	client.EXPECT().RegisterRemote(gomock.Any(), gomock.Any(), wantName, gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _, remoteURL string) error {
			gotURL = remoteURL
			return nil
		},
	)
	client.EXPECT().SavePodRemote(gomock.Any(), gomock.Any(), "my-pod", wantName, gomock.Any(), "").DoAndReturn(
		func(_ context.Context, _, _, _ string, params map[string]string, _ string) (snapshot.PodSaveResult, error) {
			gotParams = params
			return snapshot.PodSaveResult{Version: 1, Services: []string{"s3"}, Size: 42}, nil
		},
	)

	creds := snapshot.S3Credentials{AccessKeyID: "AKIA123", SecretAccessKey: "supersecret"}
	sink, getEvents := captureEvents(t)
	err := snapshot.SaveRemoteS3(context.Background(), healthyRunningMock(t), awsContainers, client, "", "my-pod", s3URL, creds, "", sink)
	require.NoError(t, err)

	// The registered URL must carry placeholders, never the secret values.
	assert.Contains(t, gotURL, "{access_key_id}")
	assert.NotContains(t, gotURL, "supersecret")
	assert.NotContains(t, gotURL, "AKIA123")
	// The secrets travel only as ephemeral params.
	assert.Equal(t, "AKIA123", gotParams["access_key_id"])
	assert.Equal(t, "supersecret", gotParams["secret_access_key"])
	_, hasToken := gotParams["session_token"]
	assert.False(t, hasToken, "session_token omitted when empty")

	var saved *output.RemoteSnapshotSavedEvent
	for _, e := range getEvents() {
		if ev, ok := e.(output.RemoteSnapshotSavedEvent); ok {
			saved = &ev
		}
	}
	require.NotNil(t, saved)
	assert.Equal(t, "my-pod", saved.PodName)
	assert.Equal(t, s3URL, saved.Location)
}

func TestSaveRemoteS3_SessionTokenIncluded(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	client := NewMockRemoteClient(ctrl)

	var gotURL string
	client.EXPECT().RegisterRemote(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _, remoteURL string) error {
			gotURL = remoteURL
			return nil
		},
	)
	client.EXPECT().SavePodRemote(gomock.Any(), gomock.Any(), "my-pod", gomock.Any(), gomock.Any(), "").DoAndReturn(
		func(_ context.Context, _, _, _ string, params map[string]string, _ string) (snapshot.PodSaveResult, error) {
			assert.Equal(t, "tok", params["session_token"])
			return snapshot.PodSaveResult{}, nil
		},
	)

	creds := snapshot.S3Credentials{AccessKeyID: "a", SecretAccessKey: "b", SessionToken: "tok"}
	err := snapshot.SaveRemoteS3(context.Background(), healthyRunningMock(t), awsContainers, client, "", "my-pod", "s3://bucket", creds, "", output.NewPlainSink(io.Discard))
	require.NoError(t, err)
	assert.Contains(t, gotURL, "session_token={session_token}")
}

func TestSaveRemoteS3_RegisterError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	client := NewMockRemoteClient(ctrl)
	client.EXPECT().RegisterRemote(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("boom"))

	err := snapshot.SaveRemoteS3(context.Background(), healthyRunningMock(t), awsContainers, client, "", "my-pod", "s3://bucket", snapshot.S3Credentials{AccessKeyID: "a", SecretAccessKey: "b"}, "", output.NewPlainSink(io.Discard))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register S3 remote")
}

func TestListRemoteS3_RendersTable(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	client := NewMockRemoteClient(ctrl)

	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)

	client.EXPECT().RegisterRemote(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	client.EXPECT().ListPodsRemote(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), "").Return(
		[]snapshot.RemotePod{{Name: "pod-a", MaxVersion: 3}, {Name: "pod-b", MaxVersion: 1}}, nil,
	)

	sink, getEvents := captureEvents(t)
	err := snapshot.ListRemoteS3(context.Background(), mockRT, awsContainers, client, "", "s3://bucket", snapshot.S3Credentials{AccessKeyID: "a", SecretAccessKey: "b"}, "", sink)
	require.NoError(t, err)

	var table *output.TableEvent
	for _, e := range getEvents() {
		if d, ok := e.(output.DeferredEvent); ok {
			if tbl, ok := d.Inner.(output.TableEvent); ok {
				table = &tbl
			}
		}
	}
	require.NotNil(t, table)
	assert.Equal(t, []string{"Name", "Version"}, table.Headers)
	assert.Len(t, table.Rows, 2)
	assert.Equal(t, []string{"pod-a", "3"}, table.Rows[0])
}
