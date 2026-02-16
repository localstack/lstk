package auth

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/output"
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
	mockStorage := NewMockAuthTokenStorage(ctrl)
	mockLogin := NewMockLoginProvider(ctrl)

	var events []any
	sink := output.SinkFunc(func(event any) {
		events = append(events, event)
	})

	auth := &Auth{
		tokenStorage: mockStorage,
		browserLogin: mockLogin,
		sink:         sink,
	}

	// Keyring returns empty (no stored token)
	mockStorage.EXPECT().GetAuthToken().Return("", errors.New("not found"))
	// Login succeeds
	mockLogin.EXPECT().Login(gomock.Any()).Return("test-token", nil)
	// Setting token in keyring fails
	mockStorage.EXPECT().SetAuthToken("test-token").Return(errors.New("keyring unavailable"))

	token, err := auth.GetToken(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "test-token", token)
	assert.Condition(t, func() bool {
		for _, event := range events {
			warningEvent, ok := event.(output.WarningEvent)
			if ok && strings.Contains(warningEvent.Message, "could not store token in keyring") {
				return true
			}
		}
		return false
	})
}
