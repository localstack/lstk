package auth

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestMain(m *testing.M) {
	_ = os.Unsetenv("LOCALSTACK_AUTH_TOKEN")
	m.Run()
}

// This test makes sure that even if the keyring store fails,
// we log a warning but still return the token obtained from login.
func TestGetToken_ReturnsTokenWhenKeyringStoreFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockKeyring := NewMockKeyring(ctrl)
	mockLogin := NewMockLoginProvider(ctrl)

	auth := &Auth{
		keyring:      mockKeyring,
		browserLogin: mockLogin,
	}

	// Keyring returns empty (no stored token)
	mockKeyring.EXPECT().Get(keyringService, keyringUser).Return("", errors.New("not found"))
	// Login succeeds
	mockLogin.EXPECT().Login(gomock.Any()).Return("test-token", nil)
	// Setting token in keyring fails
	mockKeyring.EXPECT().Set(keyringService, keyringUser, "test-token").Return(errors.New("keyring unavailable"))

	// Capture log output
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	token, err := auth.GetToken(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "test-token", token)
	assert.Contains(t, logBuf.String(), "Warning: could not store token in keyring")
}
