package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonEnvelope mirrors the shape documented in output-envelope/spec.md and
// design.md's Command Catalog, decoded loosely (Data stays raw so each test
// unmarshals it into its own command-specific shape).
type jsonEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Command       string          `json:"command"`
	Status        string          `json:"status"`
	Data          json.RawMessage `json:"data"`
	Warnings      []jsonWarning   `json:"warnings"`
	Error         *jsonError      `json:"error"`
}

type jsonWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type jsonError struct {
	Code      string `json:"code"`
	Category  string `json:"category"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// decodeEnvelope requires stdout to be exactly one well-formed JSON object,
// per the "never emits unstructured output" guarantee in output-envelope/spec.md.
func decodeEnvelope(t *testing.T, stdout string) jsonEnvelope {
	t.Helper()
	var envelope jsonEnvelope
	require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout should be exactly one JSON object: %s", stdout)
	require.NotNil(t, envelope.Warnings, "warnings should always be an array, never omitted/null")
	return envelope
}

func TestNotJSONCapableCommandRendersEnvelope(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	// login is deliberately never annotated as JSON-capable (see design.md's
	// Decisions and Non-Goals) — it's the simplest command to exercise the
	// json-flag capability's NOT_JSON_CAPABLE rejection without touching Docker.
	stdout, _, err := runLstk(t, ctx, t.TempDir(), testEnvWithHome(t.TempDir(), ""), "login", "--json")
	requireExitCode(t, 1, err)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "error", envelope.Status)
	assert.Equal(t, "login", envelope.Command)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "NOT_JSON_CAPABLE", envelope.Error.Code)
	assert.Equal(t, "USAGE", envelope.Error.Category)
}

func TestUsageErrorAfterJSONRendersEnvelope(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	// --json precedes the unknown flag, so pflag has already bound cfg.JSON to
	// true by the time it fails on --bogus-flag; SetFlagErrorFunc should render
	// this as a USAGE_ERROR envelope rather than Cobra's plain-text usage error.
	stdout, _, err := runLstk(t, ctx, t.TempDir(), testEnvWithHome(t.TempDir(), ""), "stop", "--json", "--bogus-flag")
	requireExitCode(t, 1, err)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "error", envelope.Status)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "USAGE_ERROR", envelope.Error.Code)
	assert.Equal(t, "USAGE", envelope.Error.Category)
}

func TestUsageErrorBeforeJSONFallsBackToPlainText(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	// --bogus-flag fails before pflag ever reaches --json, so cfg.JSON is still
	// false when SetFlagErrorFunc runs — this must fall back to Cobra's
	// existing plain-text usage error, not attempt to render JSON.
	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), testEnvWithHome(t.TempDir(), ""), "stop", "--bogus-flag", "--json")
	requireExitCode(t, 1, err)

	assert.Empty(t, stdout, "no JSON should be attempted when --json wasn't parsed yet")
	assert.Contains(t, stderr, "bogus-flag")
}
