package tips

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunDetector_CancelsContextAtDeadline(t *testing.T) {
	sel := testSelector(0)
	sel.deadline = 50 * time.Millisecond
	cancelled := make(chan struct{})
	tip := Tip{detect: func(ctx context.Context) (string, bool) {
		<-ctx.Done()
		close(cancelled)
		return "", false
	}}

	started := time.Now()
	_, ok := sel.runDetector(t.Context(), tip)

	assert.False(t, ok)
	assert.Less(t, time.Since(started), time.Second)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("the detector's context was not cancelled at the deadline")
	}
}

func TestRunDetector_ReturnsWithinDeadlineWhenDetectorIgnoresIt(t *testing.T) {
	sel := testSelector(0)
	sel.deadline = 50 * time.Millisecond
	tip := Tip{detect: func(context.Context) (string, bool) {
		time.Sleep(time.Minute)
		return "> Tip: too late", true
	}}

	started := time.Now()
	_, ok := sel.runDetector(t.Context(), tip)

	assert.False(t, ok)
	assert.Less(t, time.Since(started), time.Second)
}

func TestRunDetector_PanicIsDeclined(t *testing.T) {
	tip := Tip{detect: func(context.Context) (string, bool) { panic("boom") }}

	require.NotPanics(t, func() {
		_, ok := testSelector(0).runDetector(t.Context(), tip)
		assert.False(t, ok)
	})
}
