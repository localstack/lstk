package output

// This file defines the JSON shapes for the `data` field of an Envelope. They
// exist as a type-safe way of ensuring that the EnvelopeSink emits the
// current JSON field names. Shared by every command that names an emulator
// (stop, reset, ...) so "type"/"name" can't drift apart or be typo'd per command.

// JsonEmulatorRef identifies an emulator.
type JsonEmulatorRef struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// JsonEmulatorEntry is a per-emulator entry in a command's data.emulators
// list. Sealed to this package so a new per-command shape
// (JsonStartedEmulator, ...) is added deliberately here, alongside the
// others, rather than accepted as a bare `any` at the call site.
type JsonEmulatorEntry interface {
	sealedEmulatorEntry()
}

// JsonStoppedEmulator is the per-emulator entry in `stop`'s data.emulators.
type JsonStoppedEmulator struct {
	JsonEmulatorRef
	WasRunning bool `json:"wasRunning"`
}

func (JsonStoppedEmulator) sealedEmulatorEntry() {}

// JsonStatusEmulator is the per-emulator entry in `status`'s data.emulators.
// Detail fields are omitted for a non-running emulator, not nulled.
type JsonStatusEmulator struct {
	Type            string               `json:"type"`
	Running         bool                 `json:"running"`
	Health          *string              `json:"health"`
	Name            string               `json:"name,omitempty"`
	Version         string               `json:"version,omitempty"`
	Host            string               `json:"host,omitempty"`
	UptimeSeconds   *int64               `json:"uptimeSeconds,omitempty"`
	Persistence     *bool                `json:"persistence,omitempty"`
	ResourceSummary *JsonResourceSummary `json:"resourceSummary,omitempty"`
	Resources       []JsonStatusResource `json:"resources,omitempty"`
}

func (JsonStatusEmulator) sealedEmulatorEntry() {}

// JsonResourceSummary is the resource/service counts for `status --resources`.
type JsonResourceSummary struct {
	Resources int `json:"resources"`
	Services  int `json:"services"`
}

// JsonStatusResource is one deployed resource for `status --resources`.
type JsonStatusResource struct {
	Service string `json:"service"`
	Name    string `json:"name"`
	Region  string `json:"region"`
	Account string `json:"account"`
}
