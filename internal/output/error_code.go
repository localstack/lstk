package output

// ErrorCode is a stable, machine-readable identifier for a JSON envelope error.
// Values are documented in the error-codes OpenSpec capability; a call site
// with no applicable code SHALL use ErrInternal rather than inventing a new one.
type ErrorCode string

const (
	ErrRuntimeUnavailable     ErrorCode = "RUNTIME_UNAVAILABLE"
	ErrImagePullFailed        ErrorCode = "IMAGE_PULL_FAILED"
	ErrEmulatorNotRunning     ErrorCode = "EMULATOR_NOT_RUNNING"
	ErrEmulatorAlreadyRunning ErrorCode = "EMULATOR_ALREADY_RUNNING"
	ErrEmulatorWrongType      ErrorCode = "EMULATOR_WRONG_TYPE"
	ErrEmulatorNotConfigured  ErrorCode = "EMULATOR_NOT_CONFIGURED"
	ErrEmulatorStartFailed    ErrorCode = "EMULATOR_START_FAILED"
	ErrAuthRequired           ErrorCode = "AUTH_REQUIRED"
	ErrAuthLoginFailed        ErrorCode = "AUTH_LOGIN_FAILED"
	ErrCredentialsMissing     ErrorCode = "CREDENTIALS_MISSING"
	ErrLicenseInvalid         ErrorCode = "LICENSE_INVALID"
	ErrLicenseUnsupportedTag  ErrorCode = "LICENSE_UNSUPPORTED_TAG"
	ErrSnapshotNotFound       ErrorCode = "SNAPSHOT_NOT_FOUND"
	ErrSnapshotInvalidRef     ErrorCode = "SNAPSHOT_INVALID_REF"
	ErrSnapshotRemoteError    ErrorCode = "SNAPSHOT_REMOTE_ERROR"
	ErrSnapshotBucketNotFound ErrorCode = "SNAPSHOT_BUCKET_NOT_FOUND"
	ErrConfigInvalid          ErrorCode = "CONFIG_INVALID"
	ErrConfigNotFound         ErrorCode = "CONFIG_NOT_FOUND"
	ErrIntegrationNotSetUp    ErrorCode = "INTEGRATION_NOT_SET_UP"
	ErrDependencyMissing      ErrorCode = "DEPENDENCY_MISSING"
	ErrDNSResolutionRequired  ErrorCode = "DNS_RESOLUTION_REQUIRED"
	ErrConfirmationRequired   ErrorCode = "CONFIRMATION_REQUIRED"
	ErrValidationError        ErrorCode = "VALIDATION_ERROR"
	ErrUsageError             ErrorCode = "USAGE_ERROR"
	ErrNotJSONCapable         ErrorCode = "NOT_JSON_CAPABLE"
	ErrNetworkError           ErrorCode = "NETWORK_ERROR"
	ErrCancelled              ErrorCode = "CANCELLED"
	ErrInternal               ErrorCode = "INTERNAL_ERROR"
)

// retryableCodes is the single source of truth for whether a given ErrorCode
// represents a transient failure worth retrying without changing the
// invocation. Codes not listed here default to non-retryable.
var retryableCodes = map[ErrorCode]bool{
	ErrRuntimeUnavailable:  true,
	ErrImagePullFailed:     true,
	ErrEmulatorStartFailed: true,
	ErrAuthLoginFailed:     true,
	ErrSnapshotRemoteError: true,
	ErrNetworkError:        true,
	ErrCancelled:           true,
}

// Retryable reports whether the identical invocation might succeed later
// without any change to arguments or environment. It is a static property of
// the code, documented in the error-codes capability's table.
func (c ErrorCode) Retryable() bool {
	return retryableCodes[c]
}

// ErrorCategory is a coarse, small-cardinality grouping of ErrorCode values,
// additive alongside Code (not a replacement for it — see design.md's
// naming decisions). A caller that only wants to distinguish broad kinds of
// failure (an environment problem vs. a usage problem vs. an auth problem)
// can switch on the ~7 Category values instead of the ~28 Code values;
// Code remains the primary, stable identifier for anything more specific.
type ErrorCategory string

const (
	// CategoryRuntime: something outside lstk's control (Docker, network, a
	// missing external binary) is the problem.
	CategoryRuntime ErrorCategory = "RUNTIME"
	// CategoryEmulator: the emulator isn't in the state this command needs.
	CategoryEmulator ErrorCategory = "EMULATOR"
	// CategoryAuth: identity, credentials, or license/entitlement.
	CategoryAuth ErrorCategory = "AUTH"
	// CategoryResource: a referenced thing (by name or ref) doesn't exist or
	// is invalid. Named generically, not e.g. "SNAPSHOT", so a future
	// non-snapshot resource error has somewhere to land without a new category.
	CategoryResource ErrorCategory = "RESOURCE"
	// CategoryConfig: lstk's own configuration is the problem.
	CategoryConfig ErrorCategory = "CONFIG"
	// CategoryUsage: the invocation itself needs to change (flags, args,
	// confirmation, capability).
	CategoryUsage ErrorCategory = "USAGE"
	// CategoryInternal: catch-all — unexpected failure or user-initiated
	// interruption.
	CategoryInternal ErrorCategory = "INTERNAL"
)

// allErrorCodes lists every defined ErrorCode, so tests can assert every code
// has a category (categoryByCode has no sensible zero-value default, unlike
// retryableCodes, so completeness matters here in a way it doesn't there).
var allErrorCodes = []ErrorCode{
	ErrRuntimeUnavailable,
	ErrImagePullFailed,
	ErrEmulatorNotRunning,
	ErrEmulatorAlreadyRunning,
	ErrEmulatorWrongType,
	ErrEmulatorNotConfigured,
	ErrEmulatorStartFailed,
	ErrAuthRequired,
	ErrAuthLoginFailed,
	ErrCredentialsMissing,
	ErrLicenseInvalid,
	ErrLicenseUnsupportedTag,
	ErrSnapshotNotFound,
	ErrSnapshotInvalidRef,
	ErrSnapshotRemoteError,
	ErrSnapshotBucketNotFound,
	ErrConfigInvalid,
	ErrConfigNotFound,
	ErrIntegrationNotSetUp,
	ErrDependencyMissing,
	ErrDNSResolutionRequired,
	ErrConfirmationRequired,
	ErrValidationError,
	ErrUsageError,
	ErrNotJSONCapable,
	ErrNetworkError,
	ErrCancelled,
	ErrInternal,
}

// categoryByCode is the single source of truth mapping each ErrorCode to its
// static ErrorCategory, mirroring retryableCodes above. Every code in
// allErrorCodes SHALL have an entry here.
var categoryByCode = map[ErrorCode]ErrorCategory{
	ErrRuntimeUnavailable:     CategoryRuntime,
	ErrImagePullFailed:        CategoryRuntime,
	ErrDependencyMissing:      CategoryRuntime,
	ErrDNSResolutionRequired:  CategoryRuntime,
	ErrNetworkError:           CategoryRuntime,
	ErrEmulatorNotRunning:     CategoryEmulator,
	ErrEmulatorAlreadyRunning: CategoryEmulator,
	ErrEmulatorWrongType:      CategoryEmulator,
	ErrEmulatorNotConfigured:  CategoryEmulator,
	ErrEmulatorStartFailed:    CategoryEmulator,
	ErrAuthRequired:           CategoryAuth,
	ErrAuthLoginFailed:        CategoryAuth,
	ErrCredentialsMissing:     CategoryAuth,
	ErrLicenseInvalid:         CategoryAuth,
	ErrLicenseUnsupportedTag:  CategoryAuth,
	ErrSnapshotNotFound:       CategoryResource,
	ErrSnapshotInvalidRef:     CategoryResource,
	ErrSnapshotRemoteError:    CategoryResource,
	ErrSnapshotBucketNotFound: CategoryResource,
	ErrConfigInvalid:          CategoryConfig,
	ErrConfigNotFound:         CategoryConfig,
	ErrIntegrationNotSetUp:    CategoryConfig,
	ErrConfirmationRequired:   CategoryUsage,
	ErrValidationError:        CategoryUsage,
	ErrUsageError:             CategoryUsage,
	ErrNotJSONCapable:         CategoryUsage,
	ErrCancelled:              CategoryInternal,
	ErrInternal:               CategoryInternal,
}

// Category reports the code's static, coarse grouping. Every ErrorCode in
// allErrorCodes has one; a code missing from categoryByCode (which SHALL NOT
// happen) would report "" rather than panicking.
func (c ErrorCode) Category() ErrorCategory {
	return categoryByCode[c]
}
