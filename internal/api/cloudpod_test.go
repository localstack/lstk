package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/localstack/lstk/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCloudPod_FullMetadata(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pod_name": "my-baseline",
			"max_version": 2,
			"versions": [
				{"version": 1, "localstack_version": "2026.02", "services": ["s3"], "size": 100},
				{"version": 2, "localstack_version": "2026.03", "size": 49597645,
				 "description": "Pre-refactor baseline", "created_at": 1776263520,
				 "services": ["s3", "lambda", "dynamodb"],
				 "cloud_control_resources": "{\"AWS::S3::Bucket\":[{\"id\":\"a\"},{\"id\":\"b\"},{\"id\":\"c\"}],\"AWS::Lambda::Function\":[{\"id\":\"f1\"}],\"AWS::DynamoDB::Table\":[{\"id\":\"t1\"},{\"id\":\"t2\"}]}"}
			]
		}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	details, err := client.GetCloudPod(context.Background(), "test-token", "my-baseline")
	require.NoError(t, err)

	assert.Equal(t, "/v1/cloudpods/my-baseline", gotPath)
	assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(":test-token")), gotAuth)

	assert.Equal(t, "my-baseline", details.Name)
	assert.Equal(t, 2, details.Version)
	assert.Equal(t, int64(49597645), details.Size)
	assert.Equal(t, "2026.03", details.LocalStackVersion)
	assert.Equal(t, "Pre-refactor baseline", details.Message)
	assert.Equal(t, []string{"s3", "lambda", "dynamodb"}, details.Services)
	require.NotNil(t, details.Created)
	assert.Equal(t, "2026-04-15 14:32 UTC", details.Created.UTC().Format("2006-01-02 15:04 UTC"))

	// Resources are grouped by service (sorted), with pluralized nouns.
	require.Len(t, details.Resources, 3)
	assert.Equal(t, CloudPodResource{Service: "dynamodb", Counts: []CloudPodResourceCount{{Noun: "tables", Count: 2}}}, details.Resources[0])
	assert.Equal(t, CloudPodResource{Service: "lambda", Counts: []CloudPodResourceCount{{Noun: "function", Count: 1}}}, details.Resources[1])
	assert.Equal(t, CloudPodResource{Service: "s3", Counts: []CloudPodResourceCount{{Noun: "buckets", Count: 3}}}, details.Resources[2])
}

func TestGetCloudPod_NoResources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"pod_name": "bare", "max_version": 1,
			"versions": [{"version": 1, "localstack_version": "2026.03", "services": ["s3", "sqs"], "size": 2048}]}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	details, err := client.GetCloudPod(context.Background(), "tok", "bare")
	require.NoError(t, err)
	assert.Equal(t, []string{"s3", "sqs"}, details.Services)
	assert.Empty(t, details.Resources, "no cloud_control_resources should yield empty Resources, not an error")
}

func TestGetCloudPod_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetCloudPod(context.Background(), "tok", "missing")
	assert.ErrorIs(t, err, ErrCloudPodNotFound)
}

func TestGetCloudPod_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetCloudPod(context.Background(), "tok", "x")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrCloudPodNotFound)
}

func TestGetCloudPod_RFC3339Timestamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"pod_name": "iso", "max_version": 1,
			"versions": [{"version": 1, "created_at": "2026-04-15T14:32:00Z"}]}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	details, err := client.GetCloudPod(context.Background(), "tok", "iso")
	require.NoError(t, err)
	require.NotNil(t, details.Created)
	assert.Equal(t, "2026-04-15 14:32 UTC", details.Created.UTC().Format("2006-01-02 15:04 UTC"))
}

func TestPluralize(t *testing.T) {
	cases := map[string]string{
		"bucket":       "buckets",
		"function":     "functions",
		"table":        "tables",
		"queue":        "queues",
		"topic":        "topics",
		"policy":       "policies",
		"distribution": "distributions",
		"address":      "addresses",
		"key":          "keys",
	}
	for in, want := range cases {
		assert.Equal(t, want, pluralize(in), "pluralize(%q)", in)
	}
}
