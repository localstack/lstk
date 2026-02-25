// Event Usage Guide:
//
// MessageEvent (use via EmitInfo, EmitSuccess, EmitNote, EmitWarning):
//   - SeverityInfo: Transient status ("Connecting...", "Validating...")
//   - SeveritySuccess: Positive outcome ("Login successful")
//   - SeverityNote: Informational outcome ("Not currently logged in")
//   - SeverityWarning: Cautionary message ("Token expires soon")
//
// SpinnerEvent (use via EmitSpinnerStart, EmitSpinnerStop):
//   - Show loading indicator during async operations
//   - Always pair Start with Stop
//
// ErrorEvent (use via EmitError):
//   - Structured errors with title, summary, detail, and recovery actions
//   - Use for errors that need more than a single line
package output

type MessageSeverity int

const (
	SeverityInfo MessageSeverity = iota
	SeveritySuccess
	SeverityNote
	SeverityWarning
)

type MessageEvent struct {
	Severity MessageSeverity
	Text     string
}

type Event interface {
	MessageEvent | ContainerStatusEvent | ProgressEvent | UserInputRequestEvent | ContainerLogLineEvent
}

type Sink interface {
	// using any as the type only here; at call sites we'll have type safety from the union interface
	emit(event any)
}

type SinkFunc func(event any)

func (f SinkFunc) emit(event any) {
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
}

type ContainerLogLineEvent struct {
	Line string
}

// Emit sends an event to the sink with compile-time type safety via generics.
func Emit[E Event](sink Sink, event E) {
	if sink == nil {
		return
	}
	sink.emit(event)
}

func EmitInfo(sink Sink, text string) {
	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: text})
}

func EmitSuccess(sink Sink, text string) {
	Emit(sink, MessageEvent{Severity: SeveritySuccess, Text: text})
}

func EmitNote(sink Sink, text string) {
	Emit(sink, MessageEvent{Severity: SeverityNote, Text: text})
}

// Deprecated: Use EmitInfo instead
func EmitLog(sink Sink, message string) {
	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: message})
}

// Deprecated: Use EmitWarning with MessageEvent instead
func EmitWarning(sink Sink, message string) {
	Emit(sink, MessageEvent{Severity: SeverityWarning, Text: message})
}

func EmitStatus(sink Sink, phase, container, detail string) {
	Emit(sink, ContainerStatusEvent{Phase: phase, Container: container, Detail: detail})
}

func EmitProgress(sink Sink, container, layerID, status string, current, total int64) {
	Emit(sink, ProgressEvent{
		Container: container,
		LayerID:   layerID,
		Status:    status,
		Current:   current,
		Total:     total,
	})
}

func EmitUserInputRequest(sink Sink, event UserInputRequestEvent) {
	Emit(sink, event)
}

func EmitContainerLogLine(sink Sink, line string) {
	Emit(sink, ContainerLogLineEvent{Line: line})
}
