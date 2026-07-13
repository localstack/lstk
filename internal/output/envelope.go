package output

// Format identifies how an Envelope is serialized to bytes. FormatJSON is the
// only value implemented today; the type exists so a future serialization
// (e.g. YAML) is a new Format value and marshaler, not a rewrite of the
// EnvelopeSink accumulation logic below.
type Format string

// FormatJSON serializes an Envelope as compact JSON.
const FormatJSON Format = "json"

// EnvelopeSchemaVersion is the current version of the Envelope wire contract.
// Bump it only on a breaking change to the envelope itself or to a command's
// documented data shape; additive fields never require a bump.
const EnvelopeSchemaVersion = 1

const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Envelope is the common result shape every JSON-capable command emits as a
// single object to stdout. Data and Warnings are always non-nil; Error is nil
// exactly when Status is "ok".
type Envelope struct {
	SchemaVersion int            `json:"schemaVersion"`
	Command       string         `json:"command"`
	Status        string         `json:"status"`
	Data          any            `json:"data"`
	Warnings      []Warning      `json:"warnings"`
	Error         *EnvelopeError `json:"error"`
}

// Warning is a non-fatal notice surfaced alongside a successful result.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EnvelopeError is the machine-readable error shape carried by a failed
// Envelope. Code is one of the constants in error_code.go and remains the
// primary, stable identifier; Category is an additive, coarse grouping of
// Code (~7 values vs. Code's ~28) for callers that only want to distinguish
// broad kinds of failure. Both Retryable and Category are static
// classifications of Code, never computed per instance.
type EnvelopeError struct {
	Code      ErrorCode        `json:"code"`
	Category  ErrorCategory    `json:"category"`
	Message   string           `json:"message"`
	Retryable bool             `json:"retryable"`
	Details   map[string]any   `json:"details,omitempty"`
	Actions   []EnvelopeAction `json:"actions,omitempty"`
}

// EnvelopeAction is a suggested remediation for an EnvelopeError. ID is a
// stable slug a script can switch on; Command is the literal shell command a
// human or script could run.
type EnvelopeAction struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}
