package telemetry

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/localstack/lstk/internal/config"
)

const machineIDFileName = "machine_id"

// LoadOrCreateMachineID reads the persisted machine ID from disk, generating
// and writing a new one if none exists. Returns an empty string on any error
// so that telemetry can continue without a machine ID rather than failing.
func LoadOrCreateMachineID() string {
	path, err := machineIDPath()
	if err != nil {
		return ""
	}

	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	} else if !os.IsNotExist(err) {
		return ""
	}

	id := uuid.NewString()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return ""
	}
	if err := os.WriteFile(path, []byte(id), 0600); err != nil {
		return ""
	}
	return id
}

func machineIDPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, machineIDFileName), nil
}
