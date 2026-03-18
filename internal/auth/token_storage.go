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

type systemTokenStorage struct{}

func (s *systemTokenStorage) GetAuthToken() (string, error) {
	token, err := keyring.Get(keyringService, keyringAuthTokenKey)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrTokenNotFound
		}
		return "", err
	}
	return token, nil
}

func (s *systemTokenStorage) SetAuthToken(token string) error {
	return keyring.Set(keyringService, keyringAuthTokenKey, token)
}

func (s *systemTokenStorage) DeleteAuthToken() error {
	err := keyring.Delete(keyringService, keyringAuthTokenKey)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

type fallbackTokenStorage struct {
	system AuthTokenStorage
	file   AuthTokenStorage
	logger log.Logger
}

func (s *fallbackTokenStorage) GetAuthToken() (string, error) {
	token, err := s.system.GetAuthToken()
	if err == nil || errors.Is(err, ErrTokenNotFound) {
		return token, err
	}

	s.logger.Info("system keyring unavailable (%v), falling back to file-based storage", err)
	return s.file.GetAuthToken()
}

func (s *fallbackTokenStorage) SetAuthToken(token string) error {
	if err := s.system.SetAuthToken(token); err != nil {
		s.logger.Info("system keyring unavailable (%v), falling back to file-based storage", err)
		return s.file.SetAuthToken(token)
	}

	return nil
}

func (s *fallbackTokenStorage) DeleteAuthToken() error {
	if err := s.system.DeleteAuthToken(); err != nil {
		s.logger.Info("system keyring unavailable (%v), falling back to file-based storage", err)
		return s.file.DeleteAuthToken()
	}

	return nil
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

	return &fallbackTokenStorage{
		system: &systemTokenStorage{},
		file:   newFileTokenStorage(configDir),
		logger: logger,
	}, nil
}
