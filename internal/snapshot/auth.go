package snapshot

import (
	"errors"
	"strings"

	"github.com/localstack/lstk/internal/output"
)

// ErrAuthRequired indicates the emulator rejected a cloud snapshot operation
// because the identity it used was missing or not accepted.
//
// Emulator-backed pod operations (save/load/diff/remove) against a locally
// managed emulator deliberately do not pre-check for a token: when the caller
// supplies none, the running emulator reuses the identity it was started with
// (e.g. a LOCALSTACK_AUTH_TOKEN that was only present for `lstk start`).
// Externally-managed targets require caller authentication at the command
// boundary. For local targets, the verdict comes from the emulator and surfaces
// here.
var ErrAuthRequired = errors.New("authentication required for cloud snapshots")

// emitAuthRequired renders the actionable error for a pod operation the emulator
// rejected on authentication grounds, and returns the error to propagate.
//
// The emulator's own explanation is used as the summary when there is one: the
// rejection can mean a missing identity, a token that is no longer valid, or one
// without access to the snapshot, and only the emulator knows which.
func emitAuthRequired(sink output.Sink, err error) error {
	summary := "The emulator has no LocalStack identity to use for cloud snapshots"
	if detail := authRejectionDetail(err); detail != "" {
		summary = detail
	}
	sink.Emit(output.ErrorEvent{
		Title:   "Authentication failed for cloud snapshots",
		Summary: summary,
		Code:    output.ErrAuthRequired,
		Actions: []output.ErrorAction{
			{Label: "Log in:", Value: "lstk login"},
			{Label: "Or provide a valid token via the environment variable:", Value: "LOCALSTACK_AUTH_TOKEN"},
		},
	})
	return output.NewSilentError(err)
}

// authRejectionDetail returns the message the emulator gave for the rejection,
// i.e. what the client wrapped in ErrAuthRequired, or "" when it carried none.
func authRejectionDetail(err error) string {
	detail := strings.TrimPrefix(err.Error(), ErrAuthRequired.Error())
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(detail), ":"))
}
