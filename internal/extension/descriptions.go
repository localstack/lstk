package extension

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/localstack/lstk/internal/log"
	"github.com/pelletier/go-toml/v2"
)

// DescriptionsFileName is the static, hand-authored file shipped alongside
// the bundled extensions that maps a bundled extension's command name to a
// one-line description for help rendering. It is a single LocalStack-controlled
// file (not a per-extension manifest), version-locked to the bundled binaries
// and validated against them at release time.
// Its TOML body is a flat table of name = "description" entries, e.g.:
//
//	deploy = "Deploy your application to LocalStack"
//
// When the multi-call bundle (BundledBinaryName) is installed the same file is
// also the record of which commands that binary provides; see LoadBundle for
// the stricter contract that implies.
const DescriptionsFileName = "lstk-extensions.toml"

// readDescriptions parses the descriptions file at path into a
// name → description map. A read error is returned as-is so callers can test
// for os.ErrNotExist; a parse error is wrapped with the path. It is the single
// parser behind both LoadBundle (strict) and LoadDescriptions (lenient), so the
// two can never disagree on what the file says.
func readDescriptions(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	descriptions := map[string]string{}
	if err := toml.Unmarshal(data, &descriptions); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return descriptions, nil
}

// LoadDescriptions reads the bundled descriptions file from dir and returns a
// map of extension command name to one-line description. A missing or unreadable
// file degrades to an empty map without error, so help rendering never fails on
// account of descriptions. dir is the bundled-extensions directory; an empty dir
// yields an empty map. It serves the pre-bundling shape (standalone lstk-<name>
// files next to lstk); with the multi-call bundle installed, Resolver.List takes
// descriptions from the Bundle it already loaded instead.
func LoadDescriptions(dir string, logger log.Logger) map[string]string {
	if dir == "" {
		return map[string]string{}
	}
	path := filepath.Join(dir, DescriptionsFileName)
	descriptions, err := readDescriptions(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Info("extension: could not load descriptions file %s: %v", path, err)
		}
		return map[string]string{}
	}
	return descriptions
}
