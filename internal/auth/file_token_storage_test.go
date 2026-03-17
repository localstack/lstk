package auth

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileTokenStorage_SetGetDelete(t *testing.T) {
	dir := t.TempDir()
	s := newFileTokenStorage(dir)

	require.NoError(t, s.SetAuthToken("my-token"))

	got, err := s.GetAuthToken()
	require.NoError(t, err)
	assert.Equal(t, "my-token", got)

	require.NoError(t, s.DeleteAuthToken())

	_, err = s.GetAuthToken()
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestFileTokenStorage_GetReturnsErrTokenNotFoundWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	s := newFileTokenStorage(dir)

	_, err := s.GetAuthToken()
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestFileTokenStorage_DeleteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s := newFileTokenStorage(dir)

	assert.NoError(t, s.DeleteAuthToken())
	assert.NoError(t, s.DeleteAuthToken())
}

func TestFileTokenStorage_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	s := newFileTokenStorage(dir)

	require.NoError(t, s.SetAuthToken("secret"))

	info, err := os.Stat(s.path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
