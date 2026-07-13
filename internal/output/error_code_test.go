package output

import "testing"

// TestErrorCode_EveryCodeHasACategory guards the completeness invariant
// categoryByCode depends on: unlike retryableCodes (which has a sensible
// false default), a code missing from categoryByCode silently reports "",
// which is never a valid category value.
func TestErrorCode_EveryCodeHasACategory(t *testing.T) {
	t.Parallel()

	for _, code := range allErrorCodes {
		if category := code.Category(); category == "" {
			t.Errorf("ErrorCode %q has no category", code)
		}
	}
}

// TestErrorCode_AllErrorCodesIsComplete guards allErrorCodes itself against
// drifting out of sync with the const block: every code must appear exactly
// once, so TestErrorCode_EveryCodeHasACategory actually covers all of them.
func TestErrorCode_AllErrorCodesIsComplete(t *testing.T) {
	t.Parallel()

	seen := map[ErrorCode]int{}
	for _, code := range allErrorCodes {
		seen[code]++
	}
	for code, count := range seen {
		if count != 1 {
			t.Errorf("ErrorCode %q appears %d times in allErrorCodes, want exactly once", code, count)
		}
	}
	if len(allErrorCodes) != 28 {
		t.Errorf("expected 28 documented error codes, got %d — update this test's expectation alongside error-codes/spec.md if a code was intentionally added or removed", len(allErrorCodes))
	}
}

func TestErrorCode_Category(t *testing.T) {
	t.Parallel()

	cases := []struct {
		code ErrorCode
		want ErrorCategory
	}{
		{ErrRuntimeUnavailable, CategoryRuntime},
		{ErrNetworkError, CategoryRuntime},
		{ErrEmulatorNotRunning, CategoryEmulator},
		{ErrEmulatorNotConfigured, CategoryEmulator},
		{ErrAuthRequired, CategoryAuth},
		{ErrLicenseInvalid, CategoryAuth},
		{ErrSnapshotNotFound, CategoryResource},
		{ErrSnapshotBucketNotFound, CategoryResource},
		{ErrConfigInvalid, CategoryConfig},
		{ErrConfirmationRequired, CategoryUsage},
		{ErrUsageError, CategoryUsage},
		{ErrCancelled, CategoryInternal},
		{ErrInternal, CategoryInternal},
	}
	for _, c := range cases {
		if got := c.code.Category(); got != c.want {
			t.Errorf("%s.Category() = %q, want %q", c.code, got, c.want)
		}
	}
}
