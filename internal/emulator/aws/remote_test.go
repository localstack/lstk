package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterRemote(t *testing.T) {
	t.Parallel()
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient()
	err := c.RegisterRemote(context.Background(), server.Listener.Addr().String(), "lstk-s3-abc", "s3://bucket/?access_key_id={access_key_id}")
	require.NoError(t, err)
	assert.Equal(t, "/_localstack/pods/remotes/lstk-s3-abc", gotPath)
	assert.Equal(t, []any{"s3"}, gotBody["protocols"])
	assert.Equal(t, "s3://bucket/?access_key_id={access_key_id}", gotBody["remote_url"])
}

func TestSavePodRemote_SendsRemoteBody(t *testing.T) {
	t.Parallel()
	var gotBody podRequestBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &gotBody))
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprintln(w, `{"event": "completion", "status": "ok", "info": {"version": 1, "services": ["s3"], "size": 10}}`)
	}))
	defer server.Close()

	c := NewClient()
	params := map[string]string{"access_key_id": "AKIA", "secret_access_key": "shh"}
	res, err := c.SavePodRemote(context.Background(), server.Listener.Addr().String(), "my-pod", "lstk-s3-abc", params, "")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Version)
	require.NotNil(t, gotBody.Remote)
	assert.Equal(t, "lstk-s3-abc", gotBody.Remote.RemoteName)
	assert.Equal(t, "AKIA", gotBody.Remote.RemoteParams["access_key_id"])
	assert.Equal(t, "shh", gotBody.Remote.RemoteParams["secret_access_key"])
}

func TestSavePodSnapshot_SendsEmptyRemote(t *testing.T) {
	t.Parallel()
	var gotBody podRequestBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &gotBody))
		assert.Equal(t, "Basic OnRoZS10b2tlbg==", r.Header.Get("Authorization")) // base64(":the-token")
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprintln(w, `{"event": "completion", "status": "ok", "info": {"version": 2}}`)
	}))
	defer server.Close()

	c := NewClient()
	_, err := c.SavePodSnapshot(context.Background(), server.Listener.Addr().String(), "my-pod", "the-token")
	require.NoError(t, err)
	assert.Nil(t, gotBody.Remote, "platform pod save must not include a remote payload")
}

func TestListPodsRemote(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/_localstack/pods", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"cloudpods": [{"pod_name": "a", "max_version": 3}, {"pod_name": "b", "max_version": 1}]}`)
	}))
	defer server.Close()

	c := NewClient()
	pods, err := c.ListPodsRemote(context.Background(), server.Listener.Addr().String(), "lstk-s3-abc", map[string]string{"access_key_id": "x"}, "", "")
	require.NoError(t, err)
	require.Len(t, pods, 2)
	assert.Equal(t, "a", pods[0].Name)
	assert.Equal(t, 3, pods[0].MaxVersion)
}
