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
	SeveritySuccess                          // positive outcome
	SeverityNote                             // informational
	SeverityWarning                          // cautionary
	SeveritySecondary                        // subdued/decorative text
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
}

type TableEvent struct {
	Headers []string
	Rows    [][]string
}

type ResourceSummaryEvent struct {
	Resources int
	Services  int
}

// Event is a sealed marker — only event types in this package implement it,
// so Sink.Emit rejects unknown types at compile time.
type Event interface{ sealedEvent() }

func (MessageEvent) sealedEvent()          {}
func (SpinnerEvent) sealedEvent()          {}
func (ErrorEvent) sealedEvent()            {}
func (AuthEvent) sealedEvent()             {}
func (InstanceInfoEvent) sealedEvent()     {}
func (TableEvent) sealedEvent()            {}
func (ResourceSummaryEvent) sealedEvent()  {}
func (ContainerStatusEvent) sealedEvent()  {}
func (ProgressEvent) sealedEvent()         {}
func (UserInputRequestEvent) sealedEvent() {}
func (LogLineEvent) sealedEvent()          {}

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
