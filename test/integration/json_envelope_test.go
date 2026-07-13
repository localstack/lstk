package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	Code      string         `json:"code"`
	Category  string         `json:"category"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details"`
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

// TestConfigLoadFailureRendersJSONEnvelope covers PR #374's review comment:
// a config-loading failure happens in PreRunE, before RunE (and therefore
// before jsonAwareSink ever registers an EnvelopeSink) — so it must be
// rendered as a JSON envelope by a separate mechanism, not by
// wrapCommandsWithJSONEnvelope. No Docker/emulator interaction is needed: the
// malformed TOML fails to parse before stop's RunE ever runs.
func TestConfigLoadFailureRendersJSONEnvelope(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	workDir := t.TempDir()
	lstkDir := filepath.Join(workDir, ".lstk")
	require.NoError(t, os.MkdirAll(lstkDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(lstkDir, "config.toml"), []byte("[[containers]\ntype = \"aws\"\n"), 0644))

	stdout, stderr, err := runLstk(t, ctx, workDir, testEnvWithHome(t.TempDir(), ""), "stop", "--json")
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr, "the plain-text fallback in Execute() must not also fire alongside the envelope")

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "error", envelope.Status)
	assert.Equal(t, "stop", envelope.Command)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "CONFIG_INVALID", envelope.Error.Code)
	assert.Equal(t, "CONFIG", envelope.Error.Category)
	assert.False(t, envelope.Error.Retryable)
}

// TestConfigNotFoundRendersJSONEnvelope covers the CONFIG_NOT_FOUND half of
// the same PreRunE gap: an explicit --config path that doesn't exist.
func TestConfigNotFoundRendersJSONEnvelope(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	missingConfig := filepath.Join(t.TempDir(), "does-not-exist.toml")
	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), testEnvWithHome(t.TempDir(), ""),
		"--config", missingConfig, "reset", "--force", "--json",
	)
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr, "the plain-text fallback in Execute() must not also fire alongside the envelope")

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "error", envelope.Status)
	assert.Equal(t, "reset", envelope.Command)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "CONFIG_NOT_FOUND", envelope.Error.Code)
	assert.Equal(t, "CONFIG", envelope.Error.Category)
}
