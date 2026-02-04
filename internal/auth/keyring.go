package auth

//go:generate mockgen -source=keyring.go -destination=mock_keyring_test.go -package=auth

import "github.com/zalando/go-keyring"

const (
	keyringService = "localstack"
	keyringUser    = "auth-token"
)

type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (systemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (systemKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}
