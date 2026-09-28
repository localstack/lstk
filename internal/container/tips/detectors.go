package tips

import (
	"context"
	"time"
)

// detectorDeadline is the longest a start waits for the one detector it runs:
// the tip is the start's last line, so the detector delays nothing else.
const detectorDeadline = 2 * time.Second

// run a detector function, with a suitable timeout and panic recovery. Necessary
// to avoid a detector that hangs or panics from delaying or crashing the start.
// Returns the text and whether it was successful.
func (s selector) runDetector(ctx context.Context, t Tip) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, s.deadline)
	defer cancel()

	type result struct {
		text string
		ok   bool
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("tip detector panicked: %v", r)
				done <- result{}
			}
		}()
		text, ok := t.detect(ctx)
		done <- result{text, ok}
	}()

	select {
	case r := <-done:
		return r.text, r.ok && r.text != ""
	case <-ctx.Done():
		s.logger.Info("tip detector gave no answer before the deadline")
		return "", false
	}
}
