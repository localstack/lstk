package auth

import (
	"errors"
	"testing"

	"github.com/localstack/lstk/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/zalando/go-keyring"
	"go.uber.org/mock/gomock"
)

func newTestStorage(t *testing.T, kr keyringer, file AuthTokenStorage) *systemTokenStorage {
	t.Helper()
	return &systemTokenStorage{
		keyring: kr,
		file:    file,
		logger:  log.Nop(),
	}
}

func TestSystemTokenStorage_GetReturnsTokenFromKeyring(t *testing.T) {
	ctrl := gomock.NewController(t)
	kr := NewMockkeyringer(ctrl)
	file := NewMockAuthTokenStorage(ctrl)

	kr.EXPECT().Get(keyringService, keyringAuthTokenKey).Return("system-token", nil)

	token, err := newTestStorage(t, kr, file).GetAuthToken()

	assert.NoError(t, err)
	assert.Equal(t, "system-token", token)
}

func TestSystemTokenStorage_GetReturnsErrTokenNotFoundWhenKeyringEmpty(t *testing.T) {
	ctrl := gomock.NewController(t)
	kr := NewMockkeyringer(ctrl)
	file := NewMockAuthTokenStorage(ctrl)

	kr.EXPECT().Get(keyringService, keyringAuthTokenKey).Return("", keyring.ErrNotFound)

	token, err := newTestStorage(t, kr, file).GetAuthToken()

	assert.Empty(t, token)
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestSystemTokenStorage_GetFallsBackToFileWhenKeyringUnavailable(t *testing.T) {
	ctrl := gomock.NewController(t)
	kr := NewMockkeyringer(ctrl)
	file := NewMockAuthTokenStorage(ctrl)

	kr.EXPECT().Get(keyringService, keyringAuthTokenKey).Return("", errors.New("keychain unavailable"))
	file.EXPECT().GetAuthToken().Return("file-token", nil)

	token, err := newTestStorage(t, kr, file).GetAuthToken()

	assert.NoError(t, err)
	assert.Equal(t, "file-token", token)
}

func TestSystemTokenStorage_SetStoresInKeyring(t *testing.T) {
	ctrl := gomock.NewController(t)
	kr := NewMockkeyringer(ctrl)
	file := NewMockAuthTokenStorage(ctrl)

	kr.EXPECT().Set(keyringService, keyringAuthTokenKey, "token").Return(nil)

	err := newTestStorage(t, kr, file).SetAuthToken("token")

	assert.NoError(t, err)
}

func TestSystemTokenStorage_SetFallsBackToFileWhenKeyringUnavailable(t *testing.T) {
	ctrl := gomock.NewController(t)
	kr := NewMockkeyringer(ctrl)
	file := NewMockAuthTokenStorage(ctrl)

	kr.EXPECT().Set(keyringService, keyringAuthTokenKey, "token").Return(errors.New("keychain unavailable"))
	file.EXPECT().SetAuthToken("token").Return(nil)

	err := newTestStorage(t, kr, file).SetAuthToken("token")

	assert.NoError(t, err)
}

func TestSystemTokenStorage_DeleteRemovesFromKeyring(t *testing.T) {
	ctrl := gomock.NewController(t)
	kr := NewMockkeyringer(ctrl)
	file := NewMockAuthTokenStorage(ctrl)

	kr.EXPECT().Delete(keyringService, keyringAuthTokenKey).Return(nil)

	err := newTestStorage(t, kr, file).DeleteAuthToken()

	assert.NoError(t, err)
}

func TestSystemTokenStorage_DeleteSucceedsWhenKeyringTokenMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	kr := NewMockkeyringer(ctrl)
	file := NewMockAuthTokenStorage(ctrl)

	kr.EXPECT().Delete(keyringService, keyringAuthTokenKey).Return(keyring.ErrNotFound)

	err := newTestStorage(t, kr, file).DeleteAuthToken()

	assert.NoError(t, err)
}

func TestSystemTokenStorage_DeleteFallsBackToFileWhenKeyringUnavailable(t *testing.T) {
	ctrl := gomock.NewController(t)
	kr := NewMockkeyringer(ctrl)
	file := NewMockAuthTokenStorage(ctrl)

	kr.EXPECT().Delete(keyringService, keyringAuthTokenKey).Return(errors.New("keychain unavailable"))
	file.EXPECT().DeleteAuthToken().Return(nil)

	err := newTestStorage(t, kr, file).DeleteAuthToken()

	assert.NoError(t, err)
}
