package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
	})

	auth := &Auth{
		tokenStorage: mockStorage,
		login:        mockLogin,
		sink:         sink,
		allowLogin:   true,
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
			msgEvent, ok := event.(output.MessageEvent)
			if ok && msgEvent.Severity == output.SeverityWarning && strings.Contains(msgEvent.Text, "could not store token in keyring") {
				return true
			}
		}
		return false
	})
}

func TestRelogin_DiscardsTokenAndLicenseThenLogsIn(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStorage := NewMockAuthTokenStorage(ctrl)
	mockLogin := NewMockLoginProvider(ctrl)

	licensePath := filepath.Join(t.TempDir(), "license.json")
	if err := os.WriteFile(licensePath, []byte(`{"license":"stale"}`), 0600); err != nil {
		t.Fatal(err)
	}

	auth := &Auth{
		tokenStorage:    mockStorage,
		login:           mockLogin,
		sink:            output.SinkFunc(func(output.Event) {}),
		allowLogin:      true,
		licenseFilePath: licensePath,
	}

	mockStorage.EXPECT().DeleteAuthToken().Return(nil)
	mockLogin.EXPECT().Login(gomock.Any()).Return("fresh-token", nil)
	mockStorage.EXPECT().SetAuthToken("fresh-token").Return(nil)

	token, err := auth.Relogin(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "fresh-token", token)
	assert.NoFileExists(t, licensePath, "relogin must drop the cached license file")
}

func TestRelogin_FailsWhenLoginNotAllowed(t *testing.T) {
	auth := &Auth{
		sink:       output.SinkFunc(func(output.Event) {}),
		allowLogin: false,
	}

	_, err := auth.Relogin(context.Background())

	assert.ErrorContains(t, err, "authentication required")
}
