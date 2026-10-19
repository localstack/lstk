package awsconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"
)

// loadOptions marks servicesSectionName unparseable so its indented content
// round-trips through Load unchanged instead of being flattened into key=value pairs.
var loadOptions = ini.LoadOptions{UnparseableSections: []string{servicesSectionName}}

func loadINI(path string) (*ini.File, error) {
	return ini.LoadSources(loadOptions, path)
}

func sectionExists(path, sectionName string) (bool, error) {
	f, err := loadINI(path)
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

func openOrCreate(path string) (*ini.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return ini.Empty(), nil
	}
	return loadINI(path)
}

func saveAndChmod(f *ini.File, path string) error {
	if err := f.SaveTo(path); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func upsertSection(path, sectionName string, keys map[string]string) error {
	f, err := openOrCreate(path)
	if err != nil {
		return err
	}

	section := f.Section(sectionName) // gets or creates the section
	for k, v := range keys {
		section.Key(k).SetValue(v)
	}

	return saveAndChmod(f, path)
}

// upsertRawSection writes a section's raw text instead of key=value pairs.
// Needed for the services block, since upsertSection would quote a multi-line
// value instead of writing it as-is.
func upsertRawSection(path, sectionName, body string) error {
	f, err := openOrCreate(path)
	if err != nil {
		return err
	}

	if _, err := f.NewRawSection(sectionName, body); err != nil {
		return err
	}

	return saveAndChmod(f, path)
}
