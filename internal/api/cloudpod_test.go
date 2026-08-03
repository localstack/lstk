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
	details, err := client.GetCloudPod(context.Background(), "test-token", "my-baseline", 0)
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
	details, err := client.GetCloudPod(context.Background(), "tok", "bare", 0)
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
	_, err := client.GetCloudPod(context.Background(), "tok", "missing", 0)
	assert.ErrorIs(t, err, ErrCloudPodNotFound)
}

func TestGetCloudPod_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetCloudPod(context.Background(), "tok", "x", 0)
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
	details, err := client.GetCloudPod(context.Background(), "tok", "iso", 0)
	require.NoError(t, err)
	require.NotNil(t, details.Created)
	assert.Equal(t, "2026-04-15 14:32 UTC", details.Created.UTC().Format("2006-01-02 15:04 UTC"))
}

// TestGetCloudPod_PinnedVersion: every field show renders is per-version in the
// platform response, so requesting an older version reports that version's own
// metadata and resource counts — not the latest one's.
func TestGetCloudPod_PinnedVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"pod_name": "my-baseline",
			"max_version": 2,
			"versions": [
				{"version": 1, "localstack_version": "2026.02", "services": ["s3"], "size": 100,
				 "description": "first", "created_at": 1740000000,
				 "cloud_control_resources": "{\"AWS::S3::Bucket\":[{\"id\":\"a\"}]}"},
				{"version": 2, "localstack_version": "2026.03", "services": ["s3", "lambda"],
				 "storage_size": 49597645, "description": "second", "created_at": 1776263520}
			]
		}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())

	older, err := client.GetCloudPod(context.Background(), "tok", "my-baseline", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, older.Version)
	assert.Equal(t, "2026.02", older.LocalStackVersion)
	assert.Equal(t, int64(100), older.Size)
	assert.Equal(t, "first", older.Message)
	assert.Equal(t, []string{"s3"}, older.Services)
	require.Len(t, older.Resources, 1)
	assert.Equal(t, "s3", older.Resources[0].Service)

	// Version 0 still means "latest", so existing callers are unaffected.
	latest, err := client.GetCloudPod(context.Background(), "tok", "my-baseline", 0)
	require.NoError(t, err)
	assert.Equal(t, 2, latest.Version)
	assert.Equal(t, "second", latest.Message)
	assert.Empty(t, latest.Resources, "version 2 has no cloud_control_resources")
}

func TestGetCloudPod_VersionNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"pod_name": "p", "max_version": 3,
			"versions": [{"version": 3}, {"version": 2, "deleted": true}]}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())

	_, err := client.GetCloudPod(context.Background(), "tok", "p", 99)
	require.ErrorIs(t, err, ErrCloudPodVersionNotFound)
	assert.NotErrorIs(t, err, ErrCloudPodNotFound, "the pod exists; only the version does not")

	// The max version is carried structurally so callers need not parse the message.
	var versionErr *CloudPodVersionNotFoundError
	require.ErrorAs(t, err, &versionErr)
	assert.Equal(t, 99, versionErr.Version)
	assert.Equal(t, 3, versionErr.MaxVersion)
	assert.Equal(t, "p", versionErr.PodName)

	// A deleted version is treated as absent, matching what `versions` lists.
	_, err = client.GetCloudPod(context.Background(), "tok", "p", 2)
	assert.ErrorIs(t, err, ErrCloudPodVersionNotFound)
}

func TestGetCloudPodVersions_OrdersNewestFirstAndMapsFields(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		// Deliberately out of order: the platform's array order is not contractual.
		_, _ = w.Write([]byte(`{
			"pod_name": "my-baseline",
			"max_version": 3,
			"versions": [
				{"version": 2, "localstack_version": "2026.02", "services": ["s3"], "size": 100},
				{"version": 3, "localstack_version": "2026.03", "storage_size": 49597645,
				 "description": "Pre-refactor baseline", "created_at": 1776263520,
				 "services": ["s3", "lambda"]},
				{"version": 1, "localstack_version": "2026.01"}
			]
		}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	versions, err := client.GetCloudPodVersions(context.Background(), "test-token", "my-baseline")
	require.NoError(t, err)

	assert.Equal(t, "/v1/cloudpods/my-baseline", gotPath)
	assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(":test-token")), gotAuth)

	require.Len(t, versions, 3)
	assert.Equal(t, []int{3, 2, 1}, []int{versions[0].Version, versions[1].Version, versions[2].Version})

	latest := versions[0]
	assert.Equal(t, "2026.03", latest.LocalStackVersion)
	assert.Equal(t, int64(49597645), latest.Size, "storage_size should be preferred over size")
	assert.Equal(t, "Pre-refactor baseline", latest.Description)
	assert.Equal(t, []string{"s3", "lambda"}, latest.Services)
	require.NotNil(t, latest.Created)
	assert.Equal(t, "2026-04-15 14:32 UTC", latest.Created.UTC().Format("2006-01-02 15:04 UTC"))

	assert.Equal(t, int64(100), versions[1].Size, "size should be used when storage_size is absent")
	assert.Nil(t, versions[2].Created, "a version with no timestamp should have a nil Created")
}

// TestGetCloudPodVersions_FiltersDeleted mirrors the legacy CLI's untyped
// `if version.get("deleted")` check: the field has carried both a boolean flag
// and a deletion timestamp, so anything other than absent/null/false/0/"" counts
// as deleted.
func TestGetCloudPodVersions_FiltersDeleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"pod_name": "p",
			"max_version": 6,
			"versions": [
				{"version": 1},
				{"version": 2, "deleted": false},
				{"version": 3, "deleted": null},
				{"version": 4, "deleted": 0},
				{"version": 5, "deleted": ""},
				{"version": 6, "deleted": true},
				{"version": 7, "deleted": "2026-05-01T00:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	versions, err := client.GetCloudPodVersions(context.Background(), "tok", "p")
	require.NoError(t, err)

	got := make([]int, len(versions))
	for i, v := range versions {
		got[i] = v.Version
	}
	assert.Equal(t, []int{5, 4, 3, 2, 1}, got, "only versions 6 and 7 are deleted")
}

func TestGetCloudPodVersions_LastChangeFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"pod_name": "p", "max_version": 1,
			"versions": [{"version": 1, "last_change": "2026-04-15T14:32:00Z"}]}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	versions, err := client.GetCloudPodVersions(context.Background(), "tok", "p")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.NotNil(t, versions[0].Created)
	assert.Equal(t, "2026-04-15 14:32 UTC", versions[0].Created.UTC().Format("2006-01-02 15:04 UTC"))
}

func TestGetCloudPodVersions_EmptyHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"pod_name": "p", "max_version": 0, "versions": []}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	versions, err := client.GetCloudPodVersions(context.Background(), "tok", "p")
	require.NoError(t, err)
	assert.Empty(t, versions)
}

func TestGetCloudPodVersions_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetCloudPodVersions(context.Background(), "tok", "missing")
	assert.ErrorIs(t, err, ErrCloudPodNotFound)
}

func TestGetCloudPodVersions_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetCloudPodVersions(context.Background(), "tok", "x")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrCloudPodNotFound)
}

// TestGetCloudPod_IgnoresDeletedFlag pins the deliberate scope boundary: the
// deleted filter applies to the versions listing only. Changing show's
// latest-version resolution is a separate behaviour change.
func TestGetCloudPod_IgnoresDeletedFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"pod_name": "p", "max_version": 2,
			"versions": [{"version": 1}, {"version": 2, "deleted": true, "localstack_version": "2026.03"}]}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	details, err := client.GetCloudPod(context.Background(), "tok", "p", 0)
	require.NoError(t, err)
	assert.Equal(t, 2, details.Version)
	assert.Equal(t, "2026.03", details.LocalStackVersion)
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

// The platform reports "your plan doesn't include Cloud Pods" as a 403, which
// callers translate into a friendly upgrade message (DEVX-1009).
func TestCloudPods_ForbiddenMapsToPlanError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error": true, "message": "generic.forbidden"}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())

	_, listErr := client.ListCloudPods(context.Background(), "tok", "me")
	assert.ErrorIs(t, listErr, ErrCloudPodsForbidden)

	_, showErr := client.GetCloudPod(context.Background(), "tok", "any", 0)
	assert.ErrorIs(t, showErr, ErrCloudPodsForbidden)
}

// A rejected token is a 401, not a 403 — it must stay a generic error so a
// re-login problem is never reported as a billing problem.
func TestCloudPods_UnauthorizedIsNotAPlanError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": true, "message": "unauthorized"}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())

	_, listErr := client.ListCloudPods(context.Background(), "tok", "me")
	require.Error(t, listErr)
	assert.NotErrorIs(t, listErr, ErrCloudPodsForbidden)

	_, showErr := client.GetCloudPod(context.Background(), "tok", "any", 0)
	require.Error(t, showErr)
	assert.NotErrorIs(t, showErr, ErrCloudPodsForbidden)
}
