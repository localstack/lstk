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
	keyringService = "localstack"
	keyringUser    = "auth-token"
)

type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
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
		FileDir:     filepath.Join(configDir, "keyring"),
		FilePasswordFunc: func(prompt string) (string, error) {
			return "localstack-keyring", nil
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

func (k *systemKeyring) Get(service, user string) (string, error) {
	item, err := k.ring.Get(k.makeKey(service, user))
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", fmt.Errorf("credential not found")
		}
		return "", err
	}
	return string(item.Data), nil
}

func (k *systemKeyring) Set(service, user, password string) error {
	return k.ring.Set(keyring.Item{
		Key:  k.makeKey(service, user),
		Data: []byte(password),
	})
}

func (k *systemKeyring) Delete(service, user string) error {
	err := k.ring.Remove(k.makeKey(service, user))
	if errors.Is(err, keyring.ErrKeyNotFound) || os.IsNotExist(err) {
		return nil
	}
	return err
}

func (k *systemKeyring) makeKey(service, user string) string {
	return fmt.Sprintf("%s/%s", service, user)
}
