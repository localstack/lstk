package auth

//go:generate mockgen -source=token_storage.go -destination=mock_token_storage.go -package=auth

import (
	"errors"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
	"github.com/zalando/go-keyring"
)

const (
	keyringService      = "lstk"
	keyringAuthTokenKey = "lstk.auth-token"
)

var ErrTokenNotFound = errors.New("credential not found")

type AuthTokenStorage interface {
	GetAuthToken() (string, error)
	SetAuthToken(token string) error
	DeleteAuthToken() error
}

type keyringer interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type osKeyringer struct{}

func (osKeyringer) Get(service, user string) (string, error)             { return keyring.Get(service, user) }
func (osKeyringer) Set(service, user, password string) error             { return keyring.Set(service, user, password) }
func (osKeyringer) Delete(service, user string) error                    { return keyring.Delete(service, user) }

type systemTokenStorage struct {
	keyring keyringer
	file    AuthTokenStorage
	logger  log.Logger
}

func (s *systemTokenStorage) GetAuthToken() (string, error) {
	token, err := s.keyring.Get(keyringService, keyringAuthTokenKey)
	if err == nil {
		return token, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrTokenNotFound
	}
	s.logger.Info("system keyring unavailable (%v), falling back to file-based storage", err)
	return s.file.GetAuthToken()
}

func (s *systemTokenStorage) SetAuthToken(token string) error {
	if err := s.keyring.Set(keyringService, keyringAuthTokenKey, token); err != nil {
		s.logger.Info("system keyring unavailable (%v), falling back to file-based storage", err)
		return s.file.SetAuthToken(token)
	}
	return nil
}

func (s *systemTokenStorage) DeleteAuthToken() error {
	err := s.keyring.Delete(keyringService, keyringAuthTokenKey)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	s.logger.Info("system keyring unavailable (%v), falling back to file-based storage", err)
	return s.file.DeleteAuthToken()
}

func NewTokenStorage(forceFileKeyring bool, logger log.Logger) (AuthTokenStorage, error) {
	if logger == nil {
		logger = log.Nop()
	}
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}

	if forceFileKeyring {
		logger.Info("using file-based storage (forced)")
		return newFileTokenStorage(configDir), nil
	}

	return &systemTokenStorage{
		keyring: osKeyringer{},
		file:    newFileTokenStorage(configDir),
		logger:  logger,
	}, nil
}
