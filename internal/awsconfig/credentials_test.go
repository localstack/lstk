package awsconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialsFromEnv(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIA123")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_SESSION_TOKEN", "token")

	creds, err := CredentialsFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "AKIA123", creds.AccessKeyID)
	assert.Equal(t, "secret", creds.SecretAccessKey)
	assert.Equal(t, "token", creds.SessionToken)
}

func TestCredentialsFromEnv_Missing(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	_, err := CredentialsFromEnv()
	require.ErrorIs(t, err, ErrNoCredentials)
}

func TestReadProfileCredentials_FromCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials")
	require.NoError(t, os.WriteFile(credsPath, []byte(`[work]
aws_access_key_id = AKIAWORK
aws_secret_access_key = worksecret
aws_session_token = worktoken
`), 0600))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credsPath)
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config")) // absent

	creds, err := ReadProfileCredentials("work")
	require.NoError(t, err)
	assert.Equal(t, "AKIAWORK", creds.AccessKeyID)
	assert.Equal(t, "worksecret", creds.SecretAccessKey)
	assert.Equal(t, "worktoken", creds.SessionToken)
}

func TestReadProfileCredentials_FallsBackToConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(configPath, []byte(`[profile work]
aws_access_key_id = AKIACONF
aws_secret_access_key = confsecret
`), 0600))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials")) // absent
	t.Setenv("AWS_CONFIG_FILE", configPath)

	creds, err := ReadProfileCredentials("work")
	require.NoError(t, err)
	assert.Equal(t, "AKIACONF", creds.AccessKeyID)
	assert.Equal(t, "confsecret", creds.SecretAccessKey)
}

func TestReadProfileCredentials_DefaultProfileInConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(configPath, []byte(`[default]
aws_access_key_id = AKIADEF
aws_secret_access_key = defsecret
`), 0600))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials")) // absent
	t.Setenv("AWS_CONFIG_FILE", configPath)

	creds, err := ReadProfileCredentials("")
	require.NoError(t, err)
	assert.Equal(t, "AKIADEF", creds.AccessKeyID)
}

func TestReadProfileCredentials_Missing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))

	_, err := ReadProfileCredentials("ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}
