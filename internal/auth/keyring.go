package auth

//go:generate mockgen -source=keyring.go -destination=mock_keyring_test.go -package=auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/99designs/keyring"
	"github.com/localstack/lstk/internal/config"
)

const (
	keyringService        = "lstk"
	keyringAuthTokenKey   = "lstk.auth-token"
	keyringPassword       = "lstk-keyring"
	keyringFilename       = "keyring"
	keyringAuthTokenLabel = "lstk auth token"
)

type Keyring interface {
	Get(key string) (string, error)
	Set(key, password string) error
	Delete(key string) error
}

type systemKeyring struct {
	ring keyring.Keyring
}

func newSystemKeyring() (*systemKeyring, error) {
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

	// Force file backend if KEYRING env var is set to "file"
	if os.Getenv("KEYRING") == "file" {
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

	return &systemKeyring{ring: ring}, nil
}

func (k *systemKeyring) Get(key string) (string, error) {
	item, err := k.ring.Get(key)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", fmt.Errorf("credential not found")
		}
		return "", err
	}
	return string(item.Data), nil
}

func (k *systemKeyring) Set(key, password string) error {
	return k.ring.Set(keyring.Item{
		Key:   key,
		Data:  []byte(password),
		Label: keyringAuthTokenLabel,
	})
}

func (k *systemKeyring) Delete(key string) error {
	err := k.ring.Remove(key)
	if errors.Is(err, keyring.ErrKeyNotFound) || os.IsNotExist(err) {
		return nil
	}
	return err
}
