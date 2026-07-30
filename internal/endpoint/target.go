package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/spf13/cobra"
)

// FlagName is the name of the global --endpoint-url persistent flag.
const FlagName = "endpoint-url"

// EnvVar is the emulator-neutral environment variable that provides an
// endpoint URL when --endpoint-url is not passed.
const EnvVar = "LSTK_ENDPOINT_URL"

// awsEnvVar is a full synonym for EnvVar, one precedence tier lower, kept for
// users already accustomed to it from AWS tooling. It applies to every
// command that consults Resolve, not just AWS-specific ones — the
// reachability/type probe below is what protects non-AWS commands from an
// unrelated ambient value, not a restriction on which commands honor it.
const awsEnvVar = "AWS_ENDPOINT_URL"

// probeTimeout bounds how long the reachability/type probe waits for a
// response, so a hung or firewalled endpoint fails fast with an actionable
// error instead of hanging the command.
const probeTimeout = 5 * time.Second

// Target describes an externally-managed emulator resolved from
// --endpoint-url, LSTK_ENDPOINT_URL, or AWS_ENDPOINT_URL, rather than one
// discovered via local Docker inspection.
type Target struct {
	// URL is the normalized endpoint, e.g. "http://localhost:4566" (no
	// trailing slash).
	URL string
	// Type is the emulator type determined by probing the endpoint.
	Type config.EmulatorType
}

// HostPort returns the bare host:port of the target, discarding the scheme.
// Every other consumer of a resolved Target now uses the full URL (see
// Target.URL) to preserve the caller's scheme end-to-end; the sole remaining
// use is cmd/az.go's azPreflight, which feeds this into
// azureconfig.BuildEndpoint — that helper always constructs its own
// "https://<subdomain>.<host>" Azure gateway address regardless of the
// original scheme, so discarding it there is correct, not a scheme leak.
func (t *Target) HostPort() string {
	u, err := url.Parse(t.URL)
	if err != nil {
		return t.URL
	}
	return u.Host
}

// UnreachableError reports that a resolved endpoint URL did not respond, or
// did not respond with a recognizable LocalStack health payload. Its message
// deliberately never mentions Docker or suggests `lstk start` — the target is
// an emulator lstk did not start.
type UnreachableError struct {
	URL   string
	Cause error
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("could not reach LocalStack emulator at %s: %v", e.URL, e.Cause)
}

func (e *UnreachableError) Unwrap() error { return e.Cause }

// IndeterminateTypeError reports that a resolved endpoint responded, but its
// health/info payload could not be classified as aws, azure, or snowflake.
// There is no override flag or config setting for the type — detection is the
// only mechanism, so this is a hard failure rather than a fallback.
type IndeterminateTypeError struct {
	URL string
}

func (e *IndeterminateTypeError) Error() string {
	return fmt.Sprintf("could not determine the emulator type running at %s from its health response", e.URL)
}

// Resolve determines whether the user gave an endpoint URL for this
// invocation, via (in precedence order) the --endpoint-url flag,
// LSTK_ENDPOINT_URL, or AWS_ENDPOINT_URL. If none are set, it returns
// (nil, nil) so the caller falls back to existing Docker-based discovery. If
// one is set, it validates the URL and probes it for reachability and
// emulator type, returning a *Target or a descriptive error.
//
// cmd may be nil (some call sites don't have a *cobra.Command handy), in
// which case only the two environment variables are consulted.
func Resolve(ctx context.Context, cmd *cobra.Command) (*Target, error) {
	raw, ok := rawURL(cmd)
	if !ok {
		return nil, nil
	}

	normalized, err := validateURL(raw)
	if err != nil {
		return nil, err
	}

	emulatorType, err := probeType(ctx, normalized)
	if err != nil {
		return nil, err
	}

	return &Target{URL: normalized, Type: emulatorType}, nil
}

// rawURL applies the source precedence: --endpoint-url flag, LSTK_ENDPOINT_URL,
// AWS_ENDPOINT_URL (a full synonym for LSTK_ENDPOINT_URL, one tier lower).
func rawURL(cmd *cobra.Command) (string, bool) {
	_, v, ok := ResolvedSource(cmd)
	return v, ok
}

// ResolvedSource reports which endpoint URL source (if any) is present for
// this invocation — the flag itself if passed, else "LSTK_ENDPOINT_URL" or
// "AWS_ENDPOINT_URL" if set in the environment, in the same precedence order
// as Resolve — without validating or probing it.
func ResolvedSource(cmd *cobra.Command) (source, value string, ok bool) {
	if cmd != nil {
		if f := cmd.Flags().Lookup(FlagName); f != nil && f.Changed {
			if v, err := cmd.Flags().GetString(FlagName); err == nil && v != "" {
				return "--" + FlagName, v, true
			}
		}
	}
	if v := os.Getenv(EnvVar); v != "" {
		return EnvVar, v, true
	}
	if v := os.Getenv(awsEnvVar); v != "" {
		return awsEnvVar, v, true
	}
	return "", "", false
}

// validateURL requires an absolute http or https URL with a host. The
// resolved scheme is preserved end-to-end by every downstream consumer (see
// Target.URL) rather than normalized to http — https is required for
// LocalStack's cloud-hosted ephemeral instances, which are real TLS
// endpoints. Any other scheme (e.g. ftp://, ws://) is rejected here rather
// than passed through to a probe that would just fail confusingly.
func validateURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return "", fmt.Errorf("invalid --endpoint-url %q: must be an absolute URL with a scheme and host (e.g. http://localhost:4566)", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid --endpoint-url %q: only the http and https schemes are supported, got %q", raw, u.Scheme)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// healthResponse mirrors the shape of GET /_localstack/health. AWS and
// Snowflake both populate Version; Azure's health response omits it (see
// internal/emulator/azure/client.go), which is the signal to fall back to
// /_localstack/info.
type healthResponse struct {
	Version  string            `json:"version"`
	Services map[string]string `json:"services"`
}

// infoResponse mirrors the shape of GET /_localstack/info.
type infoResponse struct {
	Version string `json:"version"`
}

// awsSignatureServices are core AWS service keys used to recognize an AWS
// emulator's /_localstack/health "services" map. Confirmed against a real
// LocalStack AWS community image's health payload.
var awsSignatureServices = []string{"s3", "sqs", "sts", "iam", "lambda", "dynamodb", "cloudformation", "ec2", "kinesis"}

// probeType determines the emulator type running at endpointURL by probing
// its health/info surface, and doubles as the reachability check: an
// unreachable or non-LocalStack-shaped response fails closed rather than
// silently proceeding.
//
// NOTE: the AWS-vs-Snowflake classification below (via "services" map
// contents) is a best-effort heuristic pending confirmation against a real
// LocalStack Snowflake health payload — the Snowflake product requires a
// licensed emulator to inspect, which wasn't available to verify this
// against. See design.md's Open Questions for add-endpoint-url-flag.
func probeType(ctx context.Context, endpointURL string) (config.EmulatorType, error) {
	health, err := fetchJSON[healthResponse](ctx, endpointURL+"/_localstack/health")
	if err != nil {
		return "", &UnreachableError{URL: endpointURL, Cause: err}
	}

	if health.Version != "" {
		if t := classifyByServices(health.Services); t != "" {
			return t, nil
		}
		return "", &IndeterminateTypeError{URL: endpointURL}
	}

	// Azure's /_localstack/health omits "version" — fall back to /_localstack/info.
	info, err := fetchJSON[infoResponse](ctx, endpointURL+"/_localstack/info")
	if err != nil {
		return "", &UnreachableError{URL: endpointURL, Cause: err}
	}
	if info.Version == "" {
		return "", &IndeterminateTypeError{URL: endpointURL}
	}
	return config.EmulatorAzure, nil
}

// classifyByServices inspects a health response's "services" map for a
// per-product signature, returning "" when neither is recognized.
func classifyByServices(services map[string]string) config.EmulatorType {
	if _, ok := services["snowflake"]; ok {
		return config.EmulatorSnowflake
	}
	for _, svc := range awsSignatureServices {
		if _, ok := services[svc]; ok {
			return config.EmulatorAWS
		}
	}
	return ""
}

// fetchJSON GETs path and decodes it as a JSON T. A non-2xx status or a body
// that doesn't decode as JSON is treated the same as a transport error by the
// caller (probeType) — none of these look like a genuine LocalStack instance.
func fetchJSON[T any](ctx context.Context, url string) (T, error) {
	var zero T

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return zero, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, fmt.Errorf("decoding response from %s: %w", url, err)
	}
	return out, nil
}
