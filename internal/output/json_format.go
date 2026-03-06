package output

import "encoding/json"

type jsonEnvelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type jsonMessage struct {
	Severity string `json:"severity"`
	Text     string `json:"text"`
}

type jsonError struct {
	Title   string        `json:"title"`
	Summary string        `json:"summary,omitempty"`
	Detail  string        `json:"detail,omitempty"`
	Actions []ErrorAction `json:"actions,omitempty"`
}

type jsonContainerStatus struct {
	Phase     string `json:"phase"`
	Container string `json:"container"`
	Detail    string `json:"detail,omitempty"`
}

type jsonProgress struct {
	Container string  `json:"container"`
	LayerID   string  `json:"layer_id"`
	Status    string  `json:"status"`
	Current   int64   `json:"current"`
	Total     int64   `json:"total"`
	Percent   float64 `json:"percent,omitempty"`
}

type jsonAuth struct {
	Preamble string `json:"preamble,omitempty"`
	Code     string `json:"code,omitempty"`
	URL      string `json:"url,omitempty"`
}

type jsonLogLine struct {
	Line string `json:"line"`
}

func severityString(s MessageSeverity) string {
	switch s {
	case SeveritySuccess:
		return "success"
	case SeverityNote:
		return "note"
	case SeverityWarning:
		return "warning"
	default:
		return "info"
	}
}

// FormatEventJSON marshals an event as a JSON line. Returns nil, false for
// events that should be suppressed (e.g. spinner stop, empty progress).
func FormatEventJSON(event any) ([]byte, bool) {
	var env jsonEnvelope

	switch e := event.(type) {
	case MessageEvent:
		env = jsonEnvelope{
			Type: "message",
			Data: jsonMessage{Severity: severityString(e.Severity), Text: e.Text},
		}
	case ErrorEvent:
		env = jsonEnvelope{Type: "error", Data: jsonError(e)}
	case ContainerStatusEvent:
		env = jsonEnvelope{Type: "status", Data: jsonContainerStatus(e)}
	case ProgressEvent:
		if e.Total == 0 && e.Status == "" {
			return nil, false
		}
		p := jsonProgress{
			Container: e.Container,
			LayerID:   e.LayerID,
			Status:    e.Status,
			Current:   e.Current,
			Total:     e.Total,
		}
		if e.Total > 0 {
			p.Percent = float64(e.Current) / float64(e.Total) * 100
		}
		env = jsonEnvelope{Type: "progress", Data: p}
	case AuthEvent:
		env = jsonEnvelope{Type: "auth", Data: jsonAuth(e)}
	case ContainerLogLineEvent:
		env = jsonEnvelope{Type: "log", Data: jsonLogLine(e)}
	case SpinnerEvent:
		return nil, false
	case UserInputRequestEvent:
		return nil, false
	default:
		return nil, false
	}

	b, err := json.Marshal(env)
	if err != nil {
		return nil, false
	}
	return b, true
}
