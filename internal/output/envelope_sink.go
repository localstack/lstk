package output

import "strings"

// EnvelopeSink implements Sink by accumulating events into an Envelope
// instead of formatting lines. This accumulation step is not JSON-specific —
// it would be identical for any structured serialization; only the final
// marshal (driven by Format) differs. Presentational events (SpinnerEvent,
// ContainerStatusEvent, ProgressEvent, UserInputRequestEvent, ...) have no
// place in a single terminal result and are silently dropped.
type EnvelopeSink struct {
	format   Format
	data     map[string]any
	warnings []Warning
	err      *EnvelopeError
}

// NewEnvelopeSink returns an EnvelopeSink that serializes per format. Only
// FormatJSON exists today.
func NewEnvelopeSink(format Format) *EnvelopeSink {
	return &EnvelopeSink{format: format, data: map[string]any{}}
}

// warningCodeNotice is the fallback Warning.Code for a plain MessageEvent,
// which carries no machine-readable code of its own today.
const warningCodeNotice = "NOTICE"

func (s *EnvelopeSink) Emit(event Event) {
	switch e := event.(type) {
	case DeferredEvent:
		s.Emit(e.Inner)
	case ErrorEvent:
		s.setError(e)
	case MessageEvent:
		if e.Severity == SeverityWarning {
			s.warnings = append(s.warnings, Warning{Code: warningCodeNotice, Message: e.Text})
		}
	case EmulatorStoppedEvent:
		s.appendEmulator(map[string]any{"type": e.Type, "name": e.Name, "wasRunning": e.WasRunning})
	case EmulatorResetEvent:
		s.data["emulator"] = map[string]any{"type": e.Type, "name": e.Name}
		s.data["reset"] = true
	case UpdateCheckedEvent:
		s.data["currentVersion"] = e.CurrentVersion
		s.data["latestVersion"] = e.LatestVersion
		s.data["updateAvailable"] = e.Available
	case UpdateAppliedEvent:
		// An UpdateCheckedEvent always precedes this on the apply path (Check
		// fires it unconditionally now, for the plain-text "Update available"
		// line), so clear the keys it set rather than leaving stale
		// latestVersion/updateAvailable alongside the applied-update shape.
		delete(s.data, "latestVersion")
		delete(s.data, "updateAvailable")
		s.data["currentVersion"] = e.CurrentVersion
		s.data["updatedVersion"] = e.UpdatedVersion
		s.data["updated"] = true
		s.data["method"] = e.Method
	}
	// Every other event type is purely presentational and intentionally dropped.
}

func (s *EnvelopeSink) appendEmulator(entry map[string]any) {
	list, _ := s.data["emulators"].([]map[string]any)
	list = append(list, entry)
	s.data["emulators"] = list
}

func (s *EnvelopeSink) setError(e ErrorEvent) {
	code := e.Code
	if code == "" {
		code = ErrInternal
	}
	var actions []EnvelopeAction
	for _, a := range e.Actions {
		actions = append(actions, EnvelopeAction{ID: slugify(a.Label), Command: a.Value})
	}
	s.err = &EnvelopeError{
		Code:      code,
		Category:  code.Category(),
		Message:   e.Title,
		Retryable: code.Retryable(),
		Actions:   actions,
	}
}

// Result builds the final Envelope for command, given the error RunE
// returned. An error captured via an ErrorEvent (setError above) takes
// precedence; an error with no prior classification falls back to
// ErrInternal rather than leaving the envelope malformed.
func (s *EnvelopeSink) Result(command string, runErr error) Envelope {
	warnings := s.warnings
	if warnings == nil {
		warnings = []Warning{}
	}

	if runErr == nil {
		data := s.data
		if data == nil {
			data = map[string]any{}
		}
		return Envelope{
			SchemaVersion: EnvelopeSchemaVersion,
			Command:       command,
			Status:        StatusOK,
			Data:          data,
			Warnings:      warnings,
			Error:         nil,
		}
	}

	envErr := s.err
	if envErr == nil {
		envErr = &EnvelopeError{
			Code:      ErrInternal,
			Category:  ErrInternal.Category(),
			Message:   runErr.Error(),
			Retryable: ErrInternal.Retryable(),
		}
	}
	return Envelope{
		SchemaVersion: EnvelopeSchemaVersion,
		Command:       command,
		Status:        StatusError,
		Data:          nil,
		Warnings:      warnings,
		Error:         envErr,
	}
}

// slugify turns a plain-text ErrorAction.Label (e.g. "Start LocalStack:")
// into a stable-ish machine ID (e.g. "start-localstack"). This is a
// best-effort mechanical derivation from prose written for humans, not a
// truly independent identifier — see design.md's naming decisions.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimRight(strings.TrimSpace(s), ":"))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}
