package output

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSink struct{ events []Event }

func (r *recordingSink) Emit(e Event) { r.events = append(r.events, e) }

func TestFailEmitsTheEventAndCarriesItsCode(t *testing.T) {
	sink := &recordingSink{}
	base := errors.New("--account must be a 12-digit AWS account id")

	err := Fail(sink, ErrorEvent{Title: base.Error(), Code: ErrValidationError}, base)

	require.Len(t, sink.events, 1)
	assert.Equal(t, ErrValidationError, sink.events[0].(ErrorEvent).Code)
	assert.True(t, IsSilent(err), "the event was shown; nothing may print it again")
	assert.Equal(t, base.Error(), err.Error())
	assert.Equal(t, ErrValidationError, ErrorCodeOf(err))
}

func TestErrorCodeOfSeesThroughWrappers(t *testing.T) {
	sink := &recordingSink{}
	err := Fail(sink, ErrorEvent{Title: "x", Code: ErrRuntimeUnavailable}, errors.New("x"))

	assert.Equal(t, ErrRuntimeUnavailable, ErrorCodeOf(fmt.Errorf("checking: %w", err)))
	assert.Equal(t, ErrRuntimeUnavailable, ErrorCodeOf(&ExitCodeError{Err: err, Code: 3}))
}

func TestErrorCodeOfIsEmptyWithoutClassification(t *testing.T) {
	assert.Equal(t, ErrorCode(""), ErrorCodeOf(nil))
	assert.Equal(t, ErrorCode(""), ErrorCodeOf(errors.New("plain")))
	assert.Equal(t, ErrorCode(""), ErrorCodeOf(NewSilentError(errors.New("shown, but the site set no code"))))
}

// WithCode classifies an error the caller's display layer will still render,
// so domain code that returns errors rather than emitting them can be
// classified without being silenced.
func TestWithCodeClassifiesWithoutSilencing(t *testing.T) {
	err := WithCode(errors.New("could not list Azure clouds"), ErrInternal)

	assert.False(t, IsSilent(err), "the caller still has to render it")
	assert.Equal(t, "could not list Azure clouds", err.Error())
	assert.Equal(t, ErrInternal, ErrorCodeOf(fmt.Errorf("setup failed: %w", err)))
	assert.Nil(t, WithCode(nil, ErrInternal))
}
