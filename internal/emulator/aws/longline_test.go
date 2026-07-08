package aws

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hugeValue is larger than the 1 MB scanner buffer these NDJSON parsers used, so
// a single JSON line containing it triggers "bufio.Scanner: token too long".
var hugeValue = strings.Repeat("x", 2*1024*1024)

func TestFetchResources_HandlesLongLine(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprintf(w, `{"AWS::S3::Bucket": [{"region_name": "us-east-1", "account_id": "000000000000", "id": %q}]}`+"\n", hugeValue)
	}))
	defer server.Close()

	c := NewClient()
	rows, err := c.FetchResources(context.Background(), server.Listener.Addr().String())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, hugeValue, rows[0].Name)
}

func TestImportState_HandlesLongLine(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"service": "s3", "status": "ok", "message": %q}`+"\n", hugeValue)
	}))
	defer server.Close()

	c := NewClient()
	err := c.ImportState(context.Background(), server.Listener.Addr().String(), bytes.NewReader([]byte("{}")), "")
	require.NoError(t, err)
}

func TestSavePodSnapshot_HandlesLongLine(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"event": "progress", "status": "ok", "message": %q}`+"\n", hugeValue)
		_, _ = fmt.Fprintln(w, `{"event": "completion", "status": "ok", "info": {"version": 1, "services": ["s3"], "size": 10}}`)
	}))
	defer server.Close()

	c := NewClient()
	res, err := c.SavePodSnapshot(context.Background(), server.Listener.Addr().String(), "my-pod", "the-token")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Version)
}

func TestLoadPodSnapshot_HandlesLongLine(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"event": "service", "service": %q, "status": "ok"}`+"\n", hugeValue)
		_, _ = fmt.Fprintln(w, `{"event": "completion", "status": "ok"}`)
	}))
	defer server.Close()

	c := NewClient()
	services, err := c.LoadPodSnapshot(context.Background(), server.Listener.Addr().String(), "my-pod", "the-token", "")
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, hugeValue, services[0])
}
