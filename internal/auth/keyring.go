package auth

//go:generate mockgen -source=keyring.go -destination=mock_keyring_test.go -package=auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/99designs/keyring"
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
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	config := keyring.Config{
		ServiceName: keyringService,
		FileDir:     filepath.Join(configDir, "localstack"),
		FilePasswordFunc: func(prompt string) (string, error) {
			return "localstack-keyring", nil
		},
	}

	ring, err := keyring.Open(config)
	if err != nil {
		config.AllowedBackends = []keyring.BackendType{keyring.FileBackend}
		ring, err = keyring.Open(config)
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
