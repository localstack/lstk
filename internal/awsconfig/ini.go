package awsconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"
)

func sectionExists(path, sectionName string) (bool, error) {
	f, err := ini.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, s := range f.Sections() {
		if strings.TrimSpace(s.Name()) == sectionName {
			return true, nil
		}
	}
	return false, nil
}

func upsertSection(path, sectionName string, keys map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	var f *ini.File
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		f = ini.Empty()
	} else {
		var err error
		f, err = ini.Load(path)
		if err != nil {
			return err
		}
	}

	section := f.Section(sectionName) // gets or creates the section
	for k, v := range keys {
		section.Key(k).SetValue(v)
	}

	if err := f.SaveTo(path); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}
