package output

import (
	"errors"
	"testing"
)

func TestEnvelopeSink_SuccessWithNoEvents(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	envelope := sink.Result("stop", nil)

	if envelope.Status != StatusOK {
		t.Fatalf("expected status %q, got %q", StatusOK, envelope.Status)
	}
	if envelope.Error != nil {
		t.Fatalf("expected nil error, got %+v", envelope.Error)
	}
	if envelope.Data == nil {
		t.Fatal("expected non-nil data on success")
	}
	if envelope.Warnings == nil {
		t.Fatal("expected non-nil (possibly empty) warnings")
	}
	if envelope.SchemaVersion != EnvelopeSchemaVersion {
		t.Fatalf("expected schemaVersion %d, got %d", EnvelopeSchemaVersion, envelope.SchemaVersion)
	}
	if envelope.Command != "stop" {
		t.Fatalf("expected command %q, got %q", "stop", envelope.Command)
	}
}

func TestEnvelopeSink_EmulatorStoppedEventAccumulates(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(EmulatorStoppedEvent{Type: "aws", Name: "localstack-aws", WasRunning: true})
	sink.Emit(EmulatorStoppedEvent{Type: "snowflake", Name: "localstack-snowflake", WasRunning: true})

	envelope := sink.Result("stop", nil)
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any data, got %T", envelope.Data)
	}
	emulators, ok := data["emulators"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any emulators, got %T", data["emulators"])
	}
	if len(emulators) != 2 {
		t.Fatalf("expected 2 emulators, got %d", len(emulators))
	}
	if emulators[0]["type"] != "aws" || emulators[0]["name"] != "localstack-aws" || emulators[0]["wasRunning"] != true {
		t.Fatalf("unexpected first emulator entry: %+v", emulators[0])
	}
	if emulators[1]["type"] != "snowflake" {
		t.Fatalf("unexpected second emulator entry: %+v", emulators[1])
	}
}

func TestEnvelopeSink_EmulatorResetEvent(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(EmulatorResetEvent{Type: "aws", Name: "localstack-aws"})

	envelope := sink.Result("reset", nil)
	data := envelope.Data.(map[string]any)
	emulator, ok := data["emulator"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any emulator, got %T", data["emulator"])
	}
	if emulator["type"] != "aws" || emulator["name"] != "localstack-aws" {
		t.Fatalf("unexpected emulator entry: %+v", emulator)
	}
	if data["reset"] != true {
		t.Fatalf("expected reset: true, got %+v", data["reset"])
	}
}

func TestEnvelopeSink_UpdateCheckedEvent(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(UpdateCheckedEvent{CurrentVersion: "2.2.1", LatestVersion: "2.3.0", Available: true})

	envelope := sink.Result("update", nil)
	data := envelope.Data.(map[string]any)
	if data["currentVersion"] != "2.2.1" || data["latestVersion"] != "2.3.0" || data["updateAvailable"] != true {
		t.Fatalf("unexpected data: %+v", data)
	}
	if _, hasApplied := data["updated"]; hasApplied {
		t.Fatalf("did not expect an 'updated' key from UpdateCheckedEvent: %+v", data)
	}
}

func TestEnvelopeSink_UpdateAppliedEvent(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(UpdateAppliedEvent{CurrentVersion: "2.2.1", UpdatedVersion: "2.3.0", Method: "homebrew"})

	envelope := sink.Result("update", nil)
	data := envelope.Data.(map[string]any)
	if data["currentVersion"] != "2.2.1" || data["updatedVersion"] != "2.3.0" || data["updated"] != true || data["method"] != "homebrew" {
		t.Fatalf("unexpected data: %+v", data)
	}
}

// TestEnvelopeSink_UpdateAppliedClearsCheckedKeys covers the sequence
// update.Check/Update actually produce on the apply path: Check always fires
// UpdateCheckedEvent now (for the plain-text "Update available" line), so
// UpdateAppliedEvent must clear the keys it set rather than leaving a stale
// latestVersion/updateAvailable alongside the applied-update shape.
func TestEnvelopeSink_UpdateAppliedClearsCheckedKeys(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(UpdateCheckedEvent{CurrentVersion: "2.2.1", LatestVersion: "2.3.0", Available: true})
	sink.Emit(UpdateAppliedEvent{CurrentVersion: "2.2.1", UpdatedVersion: "2.3.0", Method: "binary"})

	envelope := sink.Result("update", nil)
	data := envelope.Data.(map[string]any)
	if data["currentVersion"] != "2.2.1" || data["updatedVersion"] != "2.3.0" || data["updated"] != true || data["method"] != "binary" {
		t.Fatalf("unexpected data: %+v", data)
	}
	if _, ok := data["latestVersion"]; ok {
		t.Fatalf("expected latestVersion to be cleared, got %+v", data)
	}
	if _, ok := data["updateAvailable"]; ok {
		t.Fatalf("expected updateAvailable to be cleared, got %+v", data)
	}
}

func TestEnvelopeSink_ErrorEventSetsClassifiedError(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(ErrorEvent{
		Title: "LocalStack is not running",
		Code:  ErrEmulatorNotRunning,
		Actions: []ErrorAction{
			{Label: "Start LocalStack:", Value: "lstk"},
		},
	})

	envelope := sink.Result("reset", errors.New("LocalStack is not running"))
	if envelope.Status != StatusError {
		t.Fatalf("expected status %q, got %q", StatusError, envelope.Status)
	}
	if envelope.Data != nil {
		t.Fatalf("expected nil data on error, got %+v", envelope.Data)
	}
	if envelope.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if envelope.Error.Code != ErrEmulatorNotRunning {
		t.Fatalf("expected code %q, got %q", ErrEmulatorNotRunning, envelope.Error.Code)
	}
	if envelope.Error.Category != CategoryEmulator {
		t.Fatalf("expected category %q, got %q", CategoryEmulator, envelope.Error.Category)
	}
	if envelope.Error.Retryable != ErrEmulatorNotRunning.Retryable() {
		t.Fatalf("expected retryable %v to match the code's static classification", ErrEmulatorNotRunning.Retryable())
	}
	if envelope.Error.Message != "LocalStack is not running" {
		t.Fatalf("expected message %q, got %q", "LocalStack is not running", envelope.Error.Message)
	}
	if len(envelope.Error.Actions) != 1 || envelope.Error.Actions[0].ID != "start-localstack" || envelope.Error.Actions[0].Command != "lstk" {
		t.Fatalf("unexpected actions: %+v", envelope.Error.Actions)
	}
}

func TestEnvelopeSink_UnclassifiedErrorFallsBackToInternal(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	envelope := sink.Result("stop", errors.New("failed to get config: boom"))

	if envelope.Status != StatusError {
		t.Fatalf("expected status %q, got %q", StatusError, envelope.Status)
	}
	if envelope.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if envelope.Error.Code != ErrInternal {
		t.Fatalf("expected fallback code %q, got %q", ErrInternal, envelope.Error.Code)
	}
	if envelope.Error.Category != CategoryInternal {
		t.Fatalf("expected fallback category %q, got %q", CategoryInternal, envelope.Error.Category)
	}
	if envelope.Error.Message != "failed to get config: boom" {
		t.Fatalf("expected message %q, got %q", "failed to get config: boom", envelope.Error.Message)
	}
}

func TestEnvelopeSink_ErrorEventWithoutCodeAlsoFallsBackToInternal(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(ErrorEvent{Title: "something went wrong"})

	envelope := sink.Result("stop", errors.New("something went wrong"))
	if envelope.Error.Code != ErrInternal {
		t.Fatalf("expected fallback code %q for an ErrorEvent with no Code set, got %q", ErrInternal, envelope.Error.Code)
	}
}

func TestEnvelopeSink_WarningsAccumulate(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(MessageEvent{Severity: SeverityWarning, Text: "DNS fallback used"})
	sink.Emit(MessageEvent{Severity: SeverityInfo, Text: "ignored, not a warning"})
	sink.Emit(MessageEvent{Severity: SeverityWarning, Text: "second warning"})

	envelope := sink.Result("update", nil)
	if len(envelope.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %+v", len(envelope.Warnings), envelope.Warnings)
	}
	if envelope.Warnings[0].Message != "DNS fallback used" || envelope.Warnings[1].Message != "second warning" {
		t.Fatalf("unexpected warnings: %+v", envelope.Warnings)
	}
}

func TestEnvelopeSink_EmptyWarningsIsEmptySliceNotNil(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	envelope := sink.Result("stop", nil)
	if envelope.Warnings == nil {
		t.Fatal("expected an empty slice, not nil")
	}
	if len(envelope.Warnings) != 0 {
		t.Fatalf("expected 0 warnings, got %d", len(envelope.Warnings))
	}
}

func TestEnvelopeSink_UnwrapsDeferredEvent(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(DeferredEvent{Inner: ErrorEvent{Title: "wrapped", Code: ErrNetworkError}})

	envelope := sink.Result("update", errors.New("wrapped"))
	if envelope.Error.Code != ErrNetworkError {
		t.Fatalf("expected DeferredEvent's inner ErrorEvent to be classified, got code %q", envelope.Error.Code)
	}
}

// dropsEvents lists presentational events an EnvelopeSink must silently drop:
// they must not appear in Data, must not panic, and must not affect Status.
func TestEnvelopeSink_DropsPresentationalEvents(t *testing.T) {
	t.Parallel()

	sink := NewEnvelopeSink(FormatJSON)
	sink.Emit(SpinnerStart("Stopping..."))
	sink.Emit(SpinnerStop())
	sink.Emit(ContainerStatusEvent{Phase: "pulling", Container: "aws"})
	sink.Emit(ProgressEvent{Container: "aws", Current: 1, Total: 2})
	sink.Emit(AuthEvent{Preamble: "hi"})
	sink.Emit(MessageEvent{Severity: SeverityInfo, Text: "just an info line"})

	envelope := sink.Result("stop", nil)
	data := envelope.Data.(map[string]any)
	if len(data) != 0 {
		t.Fatalf("expected no accumulated data from presentational events, got %+v", data)
	}
	if len(envelope.Warnings) != 0 {
		t.Fatalf("expected no warnings from a non-warning MessageEvent, got %+v", envelope.Warnings)
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"Start LocalStack:": "start-localstack",
		"See help:":         "see-help",
		"lstk -h":           "lstk-h",
		"":                  "",
	}
	for label, want := range cases {
		if got := slugify(label); got != want {
			t.Errorf("slugify(%q) = %q, want %q", label, got, want)
		}
	}
}
