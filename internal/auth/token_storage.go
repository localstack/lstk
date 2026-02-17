package auth

//go:generate mockgen -source=token_storage.go -destination=mock_token_storage.go -package=auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/99designs/keyring"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
)

const (
	keyringService        = "lstk"
	keyringAuthTokenKey   = "lstk.auth-token"
	keyringPassword       = "lstk-keyring"
	keyringFilename       = "keyring"
	keyringAuthTokenLabel = "lstk auth token"
)

type AuthTokenStorage interface {
	GetAuthToken() (string, error)
	SetAuthToken(token string) error
	DeleteAuthToken() error
}

type authTokenStorage struct {
	ring keyring.Keyring
}

func NewTokenStorage() (AuthTokenStorage, error) {
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}

	keyringConfig := keyring.Config{
		ServiceName: keyringService,
		FileDir:     filepath.Join(configDir, keyringFilename),
		FilePasswordFunc: func(prompt string) (string, error) {
			return keyringPassword, nil
		},
	}

	// Force file backend if LOCALSTACK_KEYRING env var is set to "file"
	if env.Vars.Keyring == "file" {
		keyringConfig.AllowedBackends = []keyring.BackendType{keyring.FileBackend}
	}

	ring, err := keyring.Open(keyringConfig)
	if err != nil {
		keyringConfig.AllowedBackends = []keyring.BackendType{keyring.FileBackend}
		ring, err = keyring.Open(keyringConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to open keyring: %w", err)
		}
	}

	return &authTokenStorage{ring: ring}, nil
}

func (s *authTokenStorage) GetAuthToken() (string, error) {
	item, err := s.ring.Get(keyringAuthTokenKey)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", fmt.Errorf("credential not found")
		}
		return "", err
	}
	return string(item.Data), nil
}

func (s *authTokenStorage) SetAuthToken(token string) error {
	return s.ring.Set(keyring.Item{
		Key:   keyringAuthTokenKey,
		Data:  []byte(token),
		Label: keyringAuthTokenLabel,
	})
}

func (s *authTokenStorage) DeleteAuthToken() error {
	err := s.ring.Remove(keyringAuthTokenKey)
	if errors.Is(err, keyring.ErrKeyNotFound) || os.IsNotExist(err) {
		return nil
	}
	return err
}
