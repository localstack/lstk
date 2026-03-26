package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type fileTokenStorage struct {
	path     string
	lockPath string
}

func newFileTokenStorage(configDir string) *fileTokenStorage {
	return newFileTokenStorageAtPath(filepath.Join(configDir, "auth-token"))
}

func newFileTokenStorageAtPath(filePath string) *fileTokenStorage {
	return &fileTokenStorage{
		path:     filePath,
		lockPath: filePath + ".lock",
	}
}

func (f *fileTokenStorage) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(f.lockPath), 0700); err != nil {
		return err
	}
	lf, err := os.OpenFile(f.lockPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = lf.Close() }()

	if err := lockFile(lf); err != nil {
		return err
	}
	defer func() { _ = unlockFile(lf) }()

	return fn()
}

func (f *fileTokenStorage) GetAuthToken() (string, error) {
	var token string
	err := f.withLock(func() error {
		data, err := os.ReadFile(f.path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ErrTokenNotFound
			}
			return err
		}
		token = strings.TrimSpace(string(data))
		return nil
	})
	return token, err
}

func (f *fileTokenStorage) SetAuthToken(token string) error {
	return f.withLock(func() error {
		if err := os.MkdirAll(filepath.Dir(f.path), 0700); err != nil {
			return err
		}
		return os.WriteFile(f.path, []byte(token), 0600)
	})
}

func (f *fileTokenStorage) DeleteAuthToken() error {
	return f.withLock(func() error {
		err := os.Remove(f.path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	})
}
