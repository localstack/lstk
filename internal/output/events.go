// Package output defines events for the event/sink system
//
// MessageEvent (use via sink.Emit with a MessageEvent):
//   - SeverityInfo: Transient status ("Connecting...", "Validating...")
//   - SeveritySuccess: Positive outcome ("Login successful")
//   - SeverityNote: Informational outcome ("Not currently logged in")
//   - SeverityWarning: Cautionary message ("Token expires soon")
//
// SpinnerEvent (use via output.SpinnerStart/SpinnerStop constructors):
//   - Show loading indicator during async operations
//   - Always pair Start with Stop
//
// ErrorEvent (use via sink.Emit with an ErrorEvent):
//   - Structured errors with title, summary, detail, and recovery actions
//   - Use for errors that need more than a single line
package output

import "time"

type MessageSeverity int

const (
	SeverityInfo      MessageSeverity = iota
	SeveritySuccess                   // positive outcome
	SeverityNote                      // informational
	SeverityWarning                   // cautionary
	SeveritySecondary                 // subdued/decorative text
)

type MessageEvent struct {
	Severity MessageSeverity
	Text     string
}

type SpinnerEvent struct {
	Active      bool
	Text        string
	MinDuration time.Duration // Minimum time spinner should display (0 = use default)
}

const ErrorActionPrefix = "==> "

type ErrorAction struct {
	Label string
	Value string
}

type ErrorEvent struct {
	Title   string
	Summary string
	Detail  string
	Actions []ErrorAction
}

type AuthEvent struct {
	Preamble string
	Code     string
	URL      string
}

type InstanceInfoEvent struct {
	EmulatorName  string
	Version       string
	Host          string
	ContainerName string
	Uptime        time.Duration
	Persistence   bool
}

type TableEvent struct {
	Headers []string
	Rows    [][]string
}

type ResourceSummaryEvent struct {
	Resources int
	Services  int
}

type PodSnapshotSavedEvent struct {
	PodName  string
	Version  int
	Services []string
	Size     int64
}

// RemoteSnapshotSavedEvent reports a snapshot saved to a remote storage backend
// (e.g. an S3 bucket). Location is the user-facing remote target (e.g. an s3:// URL)
// and PodName is the snapshot's identity within that remote.
type RemoteSnapshotSavedEvent struct {
	PodName  string
	Location string
	Version  int
	Services []string
	Size     int64
}

// DeferredEvent wraps another event so that the TUI renders it after the interface
// exits rather than inline. Plain sinks format the inner event immediately.
type DeferredEvent struct {
	Inner Event
}

type SnapshotLoadedEvent struct {
	Source   string   // display source shown to the user (e.g. "./snap.snapshot" or "pod:my-baseline")
	Services []string // services restored
}

type PodSnapshotRemovedEvent struct {
	PodName string
}

// SnapshotResourceCount is a count of one resource kind, e.g. {Count: 3, Noun: "buckets"}.
type SnapshotResourceCount struct {
	Count int
	Noun  string
}

// SnapshotResourceLine groups the resource counts of a single service.
type SnapshotResourceLine struct {
	Service string
	Counts  []SnapshotResourceCount
}

// SnapshotShownEvent reports the metadata of a single cloud snapshot for the
// `snapshot show` command. Created is nil and Resources is empty when the
// platform has no value for them; the formatter omits those sections.
type SnapshotShownEvent struct {
	Name              string
	Created           *time.Time
	Size              int64
	LocalStackVersion string
	Message           string
	Services          []string
	Resources         []SnapshotResourceLine
}

// SnapshotServiceSize is the byte usage of one service in a snapshot, combining
// its control-plane state (api_states/) and data-asset payloads (assets/).
type SnapshotServiceSize struct {
	Service      string `json:"service"`
	Uncompressed int64  `json:"uncompressed_bytes"`
	Compressed   int64  `json:"compressed_bytes"`
}

// SnapshotInspectedEvent reports the per-service size breakdown of a local
// snapshot file for the `snapshot inspect` command. Sizes are tallied per
// archive entry with no running emulator and no platform call; services are
// sorted largest-first.
type SnapshotInspectedEvent struct {
	Path              string                `json:"path"`
	TotalUncompressed int64                 `json:"total_uncompressed_bytes"`
	TotalCompressed   int64                 `json:"total_compressed_bytes"`
	Services          []SnapshotServiceSize `json:"services"`
}

type AuthCompleteEvent struct{}

// Event is a sealed marker — only event types in this package implement it,
// so Sink.Emit rejects unknown types at compile time.
type Event interface{ sealedEvent() }

func (MessageEvent) sealedEvent()             {}
func (SpinnerEvent) sealedEvent()             {}
func (ErrorEvent) sealedEvent()               {}
func (AuthEvent) sealedEvent()                {}
func (AuthCompleteEvent) sealedEvent()        {}
func (InstanceInfoEvent) sealedEvent()        {}
func (TableEvent) sealedEvent()               {}
func (ResourceSummaryEvent) sealedEvent()     {}
func (PodSnapshotSavedEvent) sealedEvent()    {}
func (RemoteSnapshotSavedEvent) sealedEvent() {}
func (DeferredEvent) sealedEvent()            {}
func (SnapshotLoadedEvent) sealedEvent()      {}
func (PodSnapshotRemovedEvent) sealedEvent()  {}
func (SnapshotShownEvent) sealedEvent()       {}
func (SnapshotInspectedEvent) sealedEvent()   {}
func (ContainerStatusEvent) sealedEvent()     {}
func (ProgressEvent) sealedEvent()            {}
func (UserInputRequestEvent) sealedEvent()    {}
func (PullSkippableEvent) sealedEvent()       {}
func (LogLineEvent) sealedEvent()             {}

type Sink interface {
	Emit(event Event)
}

type SinkFunc func(event Event)

func (f SinkFunc) Emit(event Event) {
	if f == nil {
		return
	}
	f(event)
}

type ContainerStatusEvent struct {
	Phase     string // e.g., "pulling", "starting", "waiting", "ready"
	Container string
	Detail    string // optional extra info (e.g., container ID)
}

type ProgressEvent struct {
	Container string
	LayerID   string
	Status    string
	Current   int64
	Total     int64
}

type InputOption struct {
	Key   string
	Label string
}

type InputResponse struct {
	SelectedKey string
	Cancelled   bool
}

type UserInputRequestEvent struct {
	Prompt     string
	Options    []InputOption
	ResponseCh chan<- InputResponse
	Vertical   bool
}

// PullSkippableEvent signals that an in-flight image pull can be abandoned in
// favor of an already-present local image. The domain emits it once real layer
// download begins (interactive mode, with a local copy present); the TUI binds
// ESC during the pull to send on SkipCh, which the domain selects on to cancel
// the pull and fall back to the local image. Never emitted in non-interactive
// mode, so PlainSink never needs to answer it.
type PullSkippableEvent struct {
	Image  string
	SkipCh chan<- struct{}
}

const (
	LogSourceEmulator = "emulator"
	LogSourceBrew     = "brew"
	LogSourceNPM      = "npm"
)

type LogLevel int

const (
	LogLevelUnknown LogLevel = iota
	LogLevelDebug
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

type LogLineEvent struct {
	Source string
	Line   string
	Level  LogLevel
}

const DefaultSpinnerMinDuration = 400 * time.Millisecond

func SpinnerStart(text string) SpinnerEvent {
	return SpinnerEvent{Active: true, Text: text, MinDuration: DefaultSpinnerMinDuration}
}

func SpinnerStartWithDuration(text string, minDuration time.Duration) SpinnerEvent {
	return SpinnerEvent{Active: true, Text: text, MinDuration: minDuration}
}

func SpinnerStop() SpinnerEvent {
	return SpinnerEvent{Active: false}
}
