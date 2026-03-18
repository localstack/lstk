package telemetry

import (
	"encoding/json"
	"runtime"

	"github.com/localstack/lstk/internal/version"
)

// Environment is the common environment block included in all telemetry events.
type Environment struct {
	LstkVersion string `json:"lstk_version"`
	AuthTokenID string `json:"auth_token_id,omitempty"`
	MachineID   string `json:"machine_id,omitempty"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
}

// LocalStackInfo mirrors the /_localstack/info response.
type LocalStackInfo struct {
	Version            string `json:"version"`
	Edition            string `json:"edition"`
	IsLicenseActivated bool   `json:"is_license_activated"`
	SessionID          string `json:"session_id"`
	MachineID          string `json:"machine_id"`
	System             string `json:"system"`
	IsDocker           bool   `json:"is_docker"`
	ServerTimeUTC      string `json:"server_time_utc"`
	Uptime             int    `json:"uptime"`
}

// CommandEvent is the payload for an lstk_command telemetry event.
type CommandEvent struct {
	Environment Environment      `json:"environment"`
	Parameters  CommandParameters `json:"parameters"`
	Result      CommandResult    `json:"result"`
}

// CommandParameters holds the command name and set flags.
type CommandParameters struct {
	Command string   `json:"command"`
	Flags   []string `json:"flags"`
}

// CommandResult holds the outcome of a command invocation.
type CommandResult struct {
	DurationMS int64  `json:"duration_ms"`
	ExitCode   int    `json:"exit_code"`
	ErrorMsg   string `json:"error_msg,omitempty"`
}

// LifecycleEvent is the payload for an lstk_lifecycle telemetry event.
type LifecycleEvent struct {
	EventType      string          `json:"event_type"`
	Environment    Environment     `json:"environment"`
	Emulator       string          `json:"emulator"`
	Image          string          `json:"image,omitempty"`
	ContainerID    string          `json:"container_id,omitempty"`
	DurationMS     int64           `json:"duration_ms,omitempty"`
	Pulled         bool            `json:"pulled,omitempty"`
	LocalStackInfo *LocalStackInfo `json:"localstack_info,omitempty"`
	ErrorCode      string          `json:"error_code,omitempty"`
	ErrorMsg       string          `json:"error_msg,omitempty"`
}

// Lifecycle event type constants.
const (
	LifecycleStartSuccess = "start_success"
	LifecycleStop         = "stop"
	LifecycleStartError   = "start_error"
)

// Error codes for start_error lifecycle events.
const (
	ErrCodePortConflict    = "port_conflict"
	ErrCodeImagePullFailed = "image_pull_failed"
	ErrCodeLicenseInvalid  = "license_invalid"
	ErrCodeStartFailed     = "start_failed"
)

// ToMap converts a telemetry event struct to a map[string]any for use with Emit.
func ToMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

// GetEnvironment returns the common environment payload for telemetry events.
func (c *Client) GetEnvironment(authTokenID string) Environment {
	env := Environment{
		LstkVersion: version.Version(),
		AuthTokenID: authTokenID,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
	}
	if c.machineID != "" {
		env.MachineID = c.machineID
	}
	return env
}
