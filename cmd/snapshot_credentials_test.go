package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeProfileCreds writes a credentials file with the given profile section and
// points AWS_SHARED_CREDENTIALS_FILE at it (config file absent).
func writeProfileCreds(t *testing.T, profile, body string) {
	t.Helper()
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials")
	require.NoError(t, os.WriteFile(credsPath, []byte("["+profile+"]\n"+body), 0600))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credsPath)
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config")) // absent
}

// clearStaticCreds unsets the static AWS credential env vars so each test starts
// from a known state.
func clearStaticCreds(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
}

func TestResolveS3Credentials_AWSProfileEnv(t *testing.T) {
	clearStaticCreds(t)
	writeProfileCreds(t, "work", "aws_access_key_id = AKIAWORK\naws_secret_access_key = worksecret\naws_session_token = worktoken\n")
	t.Setenv("AWS_PROFILE", "work")

	creds, err := resolveS3Credentials("")
	require.NoError(t, err)
	assert.Equal(t, "AKIAWORK", creds.AccessKeyID)
	assert.Equal(t, "worksecret", creds.SecretAccessKey)
	assert.Equal(t, "worktoken", creds.SessionToken)
}

func TestResolveS3Credentials_StaticEnvWinsOverAWSProfile(t *testing.T) {
	clearStaticCreds(t)
	writeProfileCreds(t, "work", "aws_access_key_id = AKIAWORK\naws_secret_access_key = worksecret\n")
	t.Setenv("AWS_PROFILE", "work")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAENV")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "envsecret")

	creds, err := resolveS3Credentials("")
	require.NoError(t, err)
	assert.Equal(t, "AKIAENV", creds.AccessKeyID)
	assert.Equal(t, "envsecret", creds.SecretAccessKey)
}

func TestResolveS3Credentials_FlagWinsOverAWSProfile(t *testing.T) {
	clearStaticCreds(t)
	writeProfileCreds(t, "flagprofile", "aws_access_key_id = AKIAFLAG\naws_secret_access_key = flagsecret\n")
	t.Setenv("AWS_PROFILE", "missing")

	creds, err := resolveS3Credentials("flagprofile")
	require.NoError(t, err)
	assert.Equal(t, "AKIAFLAG", creds.AccessKeyID)
	assert.Equal(t, "flagsecret", creds.SecretAccessKey)
}

func TestResolveS3Credentials_NoneSet(t *testing.T) {
	clearStaticCreds(t)
	dir := t.TempDir()
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials")) // absent
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))                  // absent

	_, err := resolveS3Credentials("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AWS_PROFILE")
}
