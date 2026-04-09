package config

import (
	"os"
	"path/filepath"
	"strings"
)

const planCacheFile = "plan_label"

func CachedPlanLabel() string {
	dir, err := ConfigDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, planCacheFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func CachePlanLabel(label string) {
	dir, err := ConfigDir()
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, planCacheFile), []byte(label+"\n"), 0600)
}
