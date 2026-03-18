package auth

import (
	"errors"
	"testing"

	"github.com/localstack/lstk/internal/log"
	"github.com/stretchr/testify/assert"
)

type stubTokenStorage struct {
	getToken  string
	getErr    error
	setErr    error
	deleteErr error
	setToken  string
}

func (s *stubTokenStorage) GetAuthToken() (string, error) {
	return s.getToken, s.getErr
}

func (s *stubTokenStorage) SetAuthToken(token string) error {
	s.setToken = token
	return s.setErr
}

func (s *stubTokenStorage) DeleteAuthToken() error {
	return s.deleteErr
}

func TestFallbackTokenStorage_GetUsesSystemWithoutFallbackWhenTokenMissing(t *testing.T) {
	system := &stubTokenStorage{getErr: ErrTokenNotFound}
	file := &stubTokenStorage{getToken: "file-token"}
	storage := &fallbackTokenStorage{
		system: system,
		file:   file,
		logger: log.Nop(),
	}

	token, err := storage.GetAuthToken()

	assert.Empty(t, token)
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestFallbackTokenStorage_GetFallsBackToFileWhenSystemUnavailable(t *testing.T) {
	system := &stubTokenStorage{getErr: errors.New("keychain unavailable")}
	file := &stubTokenStorage{getToken: "file-token"}
	storage := &fallbackTokenStorage{
		system: system,
		file:   file,
		logger: log.Nop(),
	}

	token, err := storage.GetAuthToken()

	assert.NoError(t, err)
	assert.Equal(t, "file-token", token)
}

func TestFallbackTokenStorage_SetFallsBackToFileWhenSystemUnavailable(t *testing.T) {
	system := &stubTokenStorage{setErr: errors.New("keychain unavailable")}
	file := &stubTokenStorage{}
	storage := &fallbackTokenStorage{
		system: system,
		file:   file,
		logger: log.Nop(),
	}

	err := storage.SetAuthToken("token")

	assert.NoError(t, err)
	assert.Equal(t, "token", file.setToken)
}
