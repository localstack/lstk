package telemetry

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	dockerclient "github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/localstack/lstk/internal/config"
)

const (
	machineIDFileName = "machine_id"
	salt              = "ls"
)

// LoadOrCreateMachineID attempts to derive a stable, anonymized machine ID by
// trying in order: Docker daemon ID, /etc/machine-id, then a persisted random UUID.
// Prefixes (dkr_, sys_, gen_) indicate origin, matching the Python implementation
// in localstack-core so IDs can be correlated across tools.
func LoadOrCreateMachineID() string {
	if id := dockerDaemonID(); id != "" {
		return "dkr_" + anonymize(id)
	}
	if id := systemMachineID(); id != "" {
		return "sys_" + anonymize(id)
	}
	return "gen_" + persistedRandomID()
}

func anonymize(physicalID string) string {
	h := md5.Sum([]byte(salt + physicalID))
	return hex.EncodeToString(h[:])[:12]
}

func dockerDaemonID() string {
	c, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return ""
	}
	defer func() { _ = c.Close() }()
	info, err := c.Info(context.Background())
	if err != nil {
		return ""
	}
	return info.ID
}

func systemMachineID() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func persistedRandomID() string {
	path, err := machineIDPath()
	if err != nil {
		return uuid.NewString()[:12]
	}

	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	} else if !os.IsNotExist(err) {
		return uuid.NewString()[:12]
	}

	id := uuid.NewString()[:12]
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return id
	}
	_ = os.WriteFile(path, []byte(id), 0600)
	return id
}

func machineIDPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, machineIDFileName), nil
}
