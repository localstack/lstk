package output

// Event is a marker interface for all event types.
type Event interface {
	event()
}

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

type LogEvent struct {
	Message string
}

func (LogEvent) event() {}

type WarningEvent struct {
	Message string
}

func (WarningEvent) event() {}

type ContainerStatusEvent struct {
	Phase     string // e.g., "pulling", "starting", "waiting", "ready"
	Container string
	Detail    string // optional extra info (e.g., container ID)
}

func (ContainerStatusEvent) event() {}

type ProgressEvent struct {
	Container string
	LayerID   string
	Status    string
	Current   int64
	Total     int64
}

func (ProgressEvent) event() {}

func Emit(sink Sink, event Event) {
	if sink == nil {
		return
	}
	sink.Emit(event)
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
