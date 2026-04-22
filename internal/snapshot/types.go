package snapshot

// PodInfo is a remote snapshot returned by the platform API.
type PodInfo struct {
	Name        string
	MaxVersion  int
	LastChange  int64 // unix timestamp
	IsPublic    bool
	StorageSize int64
	Versions    []VersionInfo
}

// VersionInfo is a single version of a remote snapshot.
type VersionInfo struct {
	Version           int
	CreatedAt         int64 // unix timestamp
	LocalStackVersion string // empty if not recorded
	Services          []string
	Description       string
	StorageSize       int64
}

// streamEvent is one line of the JSON-lines stream returned by emulator save/load/import.
type streamEvent struct {
	Event     string         `json:"event"`
	Service   string         `json:"service,omitempty"`
	Status    string         `json:"status,omitempty"`
	Operation string         `json:"operation,omitempty"`
	Message   string         `json:"message,omitempty"`
	Info      *completionInfo `json:"info,omitempty"`
}

type completionInfo struct {
	Version  int      `json:"version,omitempty"`
	Services []string `json:"services,omitempty"`
	Remote   string   `json:"remote,omitempty"`
}

// SaveResult is returned after a successful remote snapshot save.
type SaveResult struct {
	PodName  string
	Version  int
	Services []string
}

// LoadResult is returned after a successful remote snapshot load.
type LoadResult struct {
	PodName  string
	Version  int
	Services []string
}

// ExportResult is returned after a successful local export.
type ExportResult struct {
	Path     string
	Bytes    int64
	Services []string
}

// ImportResult is returned after a successful local import.
type ImportResult struct {
	Path     string
	Services []string
}

// SaveOptions configures a remote snapshot save operation.
type SaveOptions struct {
	PodName    string
	Token      string
	Services   []string
	Message    string
	Visibility string // "public", "private", or "" (unchanged)
}

// LoadOptions configures a remote snapshot load operation.
type LoadOptions struct {
	PodName  string
	Version  int    // 0 = latest
	Strategy string // "account-region-merge", "overwrite", "service-merge"
	DryRun   bool
	Token    string
}

// ExportOptions configures a local export operation.
type ExportOptions struct {
	Path     string
	Services []string
}

// ImportOptions configures a local import operation.
type ImportOptions struct {
	Path string
}
