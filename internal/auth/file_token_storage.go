package auth

import (
	"errors"
	"os"
	"path/filepath"
)

type fileTokenStorage struct {
	path string
}

func newFileTokenStorage(configDir string) *fileTokenStorage {
	return &fileTokenStorage{path: filepath.Join(configDir, "auth-token")}
}

func (f *fileTokenStorage) GetAuthToken() (string, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrTokenNotFound
		}
		return "", err
	}
	return string(data), nil
}

func (f *fileTokenStorage) SetAuthToken(token string) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(f.path, []byte(token), 0600)
}

func (f *fileTokenStorage) DeleteAuthToken() error {
	err := os.Remove(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
