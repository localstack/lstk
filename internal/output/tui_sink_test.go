package output

import (
	"reflect"
	"testing"
)

type testSender struct {
	msgs []any
}

func (s *testSender) Send(msg any) {
	s.msgs = append(s.msgs, msg)
}

func TestTUISinkForwardsEvents(t *testing.T) {
	t.Parallel()

	sender := &testSender{}
	sink := NewTUISink(sender)

	Emit(sink, LogEvent{Message: "hello"})
	Emit(sink, WarningEvent{Message: "careful"})
	Emit(sink, ContainerStatusEvent{Phase: "starting", Container: "localstack"})
	Emit(sink, ProgressEvent{LayerID: "abc", Status: "Downloading", Current: 1, Total: 2})

	want := []any{
		LogEvent{Message: "hello"},
		WarningEvent{Message: "careful"},
		ContainerStatusEvent{Phase: "starting", Container: "localstack"},
		ProgressEvent{LayerID: "abc", Status: "Downloading", Current: 1, Total: 2},
	}
	if !reflect.DeepEqual(sender.msgs, want) {
		t.Fatalf("unexpected msgs: got=%#v want=%#v", sender.msgs, want)
	}
}

func TestTUISinkNilSenderNoPanic(t *testing.T) {
	t.Parallel()

	sink := NewTUISink(nil)
	Emit(sink, LogEvent{Message: "noop"})
}
