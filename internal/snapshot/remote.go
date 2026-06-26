package snapshot

//go:generate mockgen -source=remote.go -destination=mock_remote_client_test.go -package=snapshot_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// Credential param names rendered into the remote URL template by the emulator.
// These must match the backend's S3 remote contract.
const (
	paramAccessKeyID     = "access_key_id"
	paramSecretAccessKey = "secret_access_key"
	paramSessionToken    = "session_token"
)

// S3Credentials are the AWS credentials sent to the emulator for an S3 remote.
// They are passed as ephemeral per-request parameters and never persisted.
type S3Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// params builds the remote_params map for the request body, omitting the session
// token when absent.
func (c S3Credentials) params() map[string]string {
	p := map[string]string{
		paramAccessKeyID:     c.AccessKeyID,
		paramSecretAccessKey: c.SecretAccessKey,
	}
	if c.SessionToken != "" {
		p[paramSessionToken] = c.SessionToken
	}
	return p
}

// RemotePod is a snapshot listed on a remote storage backend.
type RemotePod struct {
	Name       string
	MaxVersion int
}

// RemoteClient is satisfied by aws.Client. It manages remote registration and the
// pod operations that target a named remote.
type RemoteClient interface {
	// S3BucketExists reports whether the named S3 bucket exists. Used to reject a
	// missing bucket before an operation, rather than letting the emulator
	// auto-create it.
	S3BucketExists(ctx context.Context, bucket string) (bool, error)
	// RegisterRemote upserts a named remote on the running emulator
	// (POST /_localstack/pods/remotes/<name>). remoteURL may contain {placeholder}
	// tokens that the emulator renders with the per-request params.
	RegisterRemote(ctx context.Context, host, name, remoteURL string) error
	// SavePodRemote saves the running state to podName on the named remote.
	SavePodRemote(ctx context.Context, host, podName, remoteName string, params map[string]string, authToken string) (PodSaveResult, error)
	// LoadPodRemote loads podName from the named remote with the given merge strategy.
	LoadPodRemote(ctx context.Context, host, podName, remoteName string, params map[string]string, authToken, strategy string) ([]string, error)
	// ListPodsRemote lists the snapshots stored on the named remote.
	ListPodsRemote(ctx context.Context, host, remoteName string, params map[string]string, authToken, creator string) ([]RemotePod, error)
}

// s3RemoteBucket extracts the bucket name from an s3:// URL and reports whether
// the URL targets a local-testing endpoint (an IP or host.docker.internal), which
// mirrors the emulator's own detection. For local endpoints the bucket is the
// first path segment; for real AWS it is the host.
func s3RemoteBucket(s3URL string) (bucket string, isLocalEndpoint bool) {
	u, err := url.Parse(s3URL)
	if err != nil {
		return "", false
	}
	host := u.Hostname()
	if host == "host.docker.internal" || net.ParseIP(host) != nil {
		return strings.Trim(u.Path, "/"), true
	}
	return host, false
}

// ensureBucketExists returns an error when s3URL points at a real-AWS bucket that
// does not exist, so lstk never relies on the emulator auto-creating it. Local
// testing endpoints are skipped, and a check that cannot be performed (e.g. no
// network) is surfaced as a warning rather than a hard failure.
func ensureBucketExists(ctx context.Context, client RemoteClient, s3URL string, sink output.Sink) error {
	bucket, isLocal := s3RemoteBucket(s3URL)
	if isLocal || bucket == "" {
		return nil
	}
	exists, err := client.S3BucketExists(ctx, bucket)
	if err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not verify S3 bucket %q exists: %v", bucket, err)})
		return nil
	}
	if !exists {
		return fmt.Errorf("S3 bucket %q does not exist — create it first; lstk does not create buckets automatically", bucket)
	}
	return nil
}

// remoteName derives a deterministic registry name for an s3:// URL so the
// emulator's remote registry holds at most one entry per bucket/prefix and
// re-registration is an idempotent overwrite.
func remoteName(s3URL string) string {
	sum := sha256.Sum256([]byte(s3URL))
	return "lstk-s3-" + hex.EncodeToString(sum[:])[:10]
}

// templatedRemoteURL appends credential placeholders to the s3:// URL so the
// emulator injects the ephemeral params at runtime. Secrets never appear here —
// only "{access_key_id}"-style tokens, kept literal (not percent-encoded) so the
// backend's str.format rendering recognizes them.
func templatedRemoteURL(s3URL string, hasSessionToken bool) string {
	template := paramAccessKeyID + "={" + paramAccessKeyID + "}" +
		"&" + paramSecretAccessKey + "={" + paramSecretAccessKey + "}"
	if hasSessionToken {
		template += "&" + paramSessionToken + "={" + paramSessionToken + "}"
	}
	sep := "?"
	if strings.Contains(s3URL, "?") {
		sep = "&"
	}
	return s3URL + sep + template
}

// SaveRemoteS3 saves the running emulator's state to podName in the S3 bucket
// identified by s3URL, using the given credentials. An auth token is optional for
// S3 remotes (the S3 credentials are the auth); it is forwarded when present.
func SaveRemoteS3(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client RemoteClient, host, podName, s3URL string, creds S3Credentials, authToken string, sink output.Sink) error {
	if err := ensureBucketExists(ctx, client, s3URL, sink); err != nil {
		return err
	}
	name := remoteName(s3URL)
	remoteURL := templatedRemoteURL(s3URL, creds.SessionToken != "")
	var result PodSaveResult
	return save(ctx, rt, containers, sink,
		fmt.Sprintf("Saving snapshot to %s...", s3URL),
		func() {
			sink.Emit(output.RemoteSnapshotSavedEvent{
				PodName:  podName,
				Location: s3URL,
				Version:  result.Version,
				Services: result.Services,
				Size:     result.Size,
			})
		},
		func() error {
			if err := client.RegisterRemote(ctx, host, name, remoteURL); err != nil {
				return fmt.Errorf("register S3 remote: %w", err)
			}
			var err error
			result, err = client.SavePodRemote(ctx, host, podName, name, creds.params(), authToken)
			return err
		},
	)
}

// LoadRemoteS3 loads podName from the S3 bucket identified by s3URL into the
// running emulator, starting it first if needed.
func LoadRemoteS3(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client RemoteClient, host, podName, s3URL string, creds S3Credentials, authToken, strategy string, starter Starter, sink output.Sink) error {
	if err := ensureBucketExists(ctx, client, s3URL, sink); err != nil {
		return err
	}
	name := remoteName(s3URL)
	remoteURL := templatedRemoteURL(s3URL, creds.SessionToken != "")
	var services []string
	return load(ctx, rt, containers, sink, starter,
		fmt.Sprintf("Loading snapshot %q from %s...", podName, s3URL),
		func() {
			sink.Emit(output.SnapshotLoadedEvent{
				Source:   fmt.Sprintf("%s (%s)", s3URL, podName),
				Services: services,
			})
		},
		func() error {
			if err := client.RegisterRemote(ctx, host, name, remoteURL); err != nil {
				return fmt.Errorf("register S3 remote: %w", err)
			}
			var err error
			services, err = client.LoadPodRemote(ctx, host, podName, name, creds.params(), authToken, strategy)
			return err
		},
	)
}

// ListRemoteS3 lists the snapshots stored in the S3 bucket identified by s3URL.
// Unlike List (which queries the platform API), this requires a running emulator
// because the emulator performs the S3 listing.
func ListRemoteS3(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client RemoteClient, host, s3URL string, creds S3Credentials, authToken string, sink output.Sink) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	if err := ensureBucketExists(ctx, client, s3URL, sink); err != nil {
		return err
	}

	name := remoteName(s3URL)
	remoteURL := templatedRemoteURL(s3URL, creds.SessionToken != "")

	sink.Emit(output.SpinnerStart("Fetching snapshots"))
	if err := client.RegisterRemote(ctx, host, name, remoteURL); err != nil {
		sink.Emit(output.SpinnerStop())
		return fmt.Errorf("register S3 remote: %w", err)
	}
	pods, err := client.ListPodsRemote(ctx, host, name, creds.params(), authToken, "")
	sink.Emit(output.SpinnerStop())
	if err != nil {
		return fmt.Errorf("list snapshots on %s: %w", s3URL, err)
	}

	if len(pods) == 0 {
		sink.Emit(output.DeferredEvent{Inner: output.MessageEvent{Severity: output.SeverityNote, Text: fmt.Sprintf("No snapshots found on %s", s3URL)}})
		return nil
	}
	noun := "snapshots"
	if len(pods) == 1 {
		noun = "snapshot"
	}
	rows := make([][]string, len(pods))
	for i, p := range pods {
		rows[i] = []string{p.Name, fmt.Sprintf("%d", p.MaxVersion)}
	}
	sink.Emit(output.DeferredEvent{Inner: output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("~ %d %s\n", len(pods), noun)}})
	sink.Emit(output.DeferredEvent{Inner: output.TableEvent{
		Headers: []string{"Name", "Version"},
		Rows:    rows,
	}})
	return nil
}
