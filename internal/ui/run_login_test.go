package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type testModelSender struct {
	tm *teatest.TestModel
}

func (s testModelSender) Send(msg any) {
	s.tm.Send(msg)
}

func createMockAPIServer(t *testing.T, licenseToken string, confirmed bool) *httptest.Server {
	authReqID := "test-auth-req-id"
	exchangeToken := "test-exchange-token"
	bearerToken := "Bearer test-bearer-token"

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/auth/request":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":             authReqID,
				"code":           "TEST123",
				"exchange_token": exchangeToken,
			})

		case r.Method == "GET" && r.URL.Path == fmt.Sprintf("/v1/auth/request/%s", authReqID):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]bool{
				"confirmed": confirmed,
			})

		case r.Method == "POST" && r.URL.Path == fmt.Sprintf("/v1/auth/request/%s/exchange", authReqID):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":         authReqID,
				"auth_token": bearerToken,
			})

		case r.Method == "GET" && r.URL.Path == "/v1/license/credentials":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"token": licenseToken,
			})

		case r.Method == "POST" && r.URL.Path == "/v1/license/request":
			w.WriteHeader(http.StatusOK)

		default:
			t.Logf("Unhandled request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func readOutput(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return string(b)
}

func TestLoginFlow_DeviceFlowSuccess(t *testing.T) {
	mockServer := createMockAPIServer(t, "test-license-token", true)
	defer mockServer.Close()

	t.Setenv("LOCALSTACK_API_ENDPOINT", mockServer.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ctrl := gomock.NewController(t)
	mockStorage := auth.NewMockAuthTokenStorage(ctrl)
	mockStorage.EXPECT().GetAuthToken().Return("", errors.New("no token"))
	mockStorage.EXPECT().SetAuthToken(gomock.Any()).Return(nil)

	tm := teatest.NewTestModel(t, NewApp("test", cancel), teatest.WithInitialTermSize(120, 40))
	sender := testModelSender{tm: tm}
	platformClient := api.NewPlatformClient()

	errCh := make(chan error, 1)
	go func() {
		a := auth.New(output.NewTUISink(sender), platformClient, mockStorage, true)
		_, err := a.GetToken(ctx)
		errCh <- err
		if err != nil && !errors.Is(err, context.Canceled) {
			tm.Send(runErrMsg{err: err})
		} else {
			tm.Send(runDoneMsg{})
		}
	}()

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("TEST123"))
	}, teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case err := <-errCh:
		require.NoError(t, err, "login should succeed")
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for login")
	}

	tm.Send(tea.QuitMsg{})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))

	out := readOutput(tm.FinalOutput(t))
	assert.Contains(t, out, "Login successful")
}

func TestLoginFlow_DeviceFlowFailure_NotConfirmed(t *testing.T) {
	mockServer := createMockAPIServer(t, "", false)
	defer mockServer.Close()

	t.Setenv("LOCALSTACK_API_ENDPOINT", mockServer.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ctrl := gomock.NewController(t)
	mockStorage := auth.NewMockAuthTokenStorage(ctrl)
	mockStorage.EXPECT().GetAuthToken().Return("", errors.New("no token"))

	tm := teatest.NewTestModel(t, NewApp("test", cancel), teatest.WithInitialTermSize(120, 40))
	sender := testModelSender{tm: tm}
	platformClient := api.NewPlatformClient()

	errCh := make(chan error, 1)
	go func() {
		a := auth.New(output.NewTUISink(sender), platformClient, mockStorage, true)
		_, err := a.GetToken(ctx)
		errCh <- err
		if err != nil && !errors.Is(err, context.Canceled) {
			tm.Send(runErrMsg{err: err})
		} else {
			tm.Send(runDoneMsg{})
		}
	}()

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("TEST123"))
	}, teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case err := <-errCh:
		require.Error(t, err, "login should fail")
		assert.Contains(t, err.Error(), "not confirmed")
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for login")
	}

	tm.Send(tea.QuitMsg{})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))

	out := readOutput(tm.FinalOutput(t))
	assert.Contains(t, out, "Authentication failed")
}

func TestLoginFlow_BrowserCallback(t *testing.T) {
	mockServer := createMockAPIServer(t, "test-license-token", true)
	defer mockServer.Close()

	t.Setenv("LOCALSTACK_API_ENDPOINT", mockServer.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ctrl := gomock.NewController(t)
	mockStorage := auth.NewMockAuthTokenStorage(ctrl)
	mockStorage.EXPECT().GetAuthToken().Return("", errors.New("no token"))
	mockStorage.EXPECT().SetAuthToken(gomock.Any()).Return(nil)

	tm := teatest.NewTestModel(t, NewApp("test", cancel), teatest.WithInitialTermSize(120, 40))
	sender := testModelSender{tm: tm}
	platformClient := api.NewPlatformClient()

	errCh := make(chan error, 1)
	go func() {
		a := auth.New(output.NewTUISink(sender), platformClient, mockStorage, true)
		_, err := a.GetToken(ctx)
		errCh <- err
		if err != nil && !errors.Is(err, context.Canceled) {
			tm.Send(runErrMsg{err: err})
		} else {
			tm.Send(runDoneMsg{})
		}
	}()

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("TEST123"))
	}, teatest.WithDuration(5*time.Second))

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:45678/auth/success?token=browser-token")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	select {
	case err := <-errCh:
		require.NoError(t, err, "login should succeed via browser callback")
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for login")
	}

	tm.Send(tea.QuitMsg{})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))

	out := readOutput(tm.FinalOutput(t))
	assert.Contains(t, out, "Login successful")
}
