package extension

import (
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
const DescriptionsFileName = "lstk-extensions.toml"

// LoadDescriptions reads the bundled descriptions file from dir and returns a
// map of extension command name to one-line description. A missing or unreadable
// file degrades to an empty map without error, so help rendering never fails on
// account of descriptions. dir is the bundled-extensions directory; an empty dir
// yields an empty map.
func LoadDescriptions(dir string, logger log.Logger) map[string]string {
	if dir == "" {
		return map[string]string{}
	}
	path := filepath.Join(dir, DescriptionsFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Info("extension: could not read descriptions file %s: %v", path, err)
		}
		return map[string]string{}
	}
	descriptions := map[string]string{}
	if err := toml.Unmarshal(data, &descriptions); err != nil {
		logger.Info("extension: could not parse descriptions file %s: %v", path, err)
		return map[string]string{}
	}
	return descriptions
}
