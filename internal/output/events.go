package output

type Event interface {
	LogEvent | WarningEvent | ContainerStatusEvent | ProgressEvent | UserInputRequestEvent | ContainerLogLineEvent
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

type LogEvent struct {
	Message string
}

type WarningEvent struct {
	Message string
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

func EmitLog(sink Sink, message string) {
	Emit(sink, LogEvent{Message: message})
}

func EmitWarning(sink Sink, message string) {
	Emit(sink, WarningEvent{Message: message})
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
