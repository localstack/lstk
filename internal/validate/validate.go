// Package validate provides reusable, deterministic validators for user-supplied
// CLI inputs. It exists to make the CLI a safe target for AI agents and scripts,
// which can produce malformed or hostile input — control characters, path
// traversal, percent-encoding, embedded query parameters, shell metacharacters —
// in ways humans rarely do.
//
// Validators return an *Error carrying a machine-classifiable Rule so callers can
// surface a precise, stable reason (and, in JSON output mode, a stable error
// code) instead of a generic "invalid input" message. Error() returns the bare
// reason so it composes cleanly when wrapped with caller context.
package validate

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Rule classifies why a value was rejected. The values are stable and intended to
// be surfaced as machine-readable error codes.
const (
	RuleEmpty        = "empty"
	RuleControlChars = "control_chars"
	RuleEncoding     = "encoding"
	RuleEmbedded     = "embedded"
	RuleMetachars    = "metachars"
	RuleFormat       = "format"
	RuleRange        = "range"
)

type Error struct {
	Field string
	Rule  string
	Msg   string
}

func (e *Error) Error() string { return e.Msg }

func newError(field, rule, msg string) *Error {
	return &Error{Field: field, Rule: rule, Msg: msg}
}

// containsControlChars reports whether s contains any control character other
// than tab, newline, or carriage return.
func containsControlChars(s string) bool {
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func NoControlChars(field, value string) error {
	if containsControlChars(value) {
		return newError(field, RuleControlChars, "contains control characters")
	}
	return nil
}

var envVarKeyRegexp = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// EnvVarName validates an environment variable name (the key of a KEY=VALUE pair).
func EnvVarName(name string) error {
	if !envVarKeyRegexp.MatchString(name) {
		return newError("env", RuleFormat, fmt.Sprintf("env key %q contains invalid characters", name))
	}
	return nil
}

// podNameRegexp mirrors the platform API's POD_NAME_PATTERN (also enforced
// identically by the emulator's pods bootstrap): letters, digits, underscores,
// and hyphens only. Anything else — including dots — is rejected server-side
// with HTTP 400 "Invalid name for cloud pod", so accepting it here would only
// defer the failure.
var podNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// shellMetaChars are characters that enable command injection if a pod name is
// ever interpolated into a shell. The slash, question mark, and hash are handled
// separately as embedded path/query characters and are not repeated here.
const shellMetaChars = ";&|$\x60<>(){}[]!*'~\\\""

// PodName validates a Cloud Pod name against the platform's contract. The
// platform pattern has no length bound; the 128-character cap here is a local
// sanity limit against absurd inputs, not part of the platform contract.
// It runs ordered deny-checks so the most specific reason wins, then the
// allow-list. The deny-checks exist to give precise, machine-classifiable
// feedback; the allow-list alone would reject every invalid value.
func PodName(value string) error {
	const field = "pod name"
	switch {
	case value == "":
		return newError(field, RuleEmpty, "must not be empty")
	case containsControlChars(value):
		return newError(field, RuleControlChars, "contains control characters")
	case strings.Contains(value, "%"):
		return newError(field, RuleEncoding, "contains percent-encoding (pass the decoded value)")
	case strings.ContainsAny(value, "/?#"):
		return newError(field, RuleEmbedded, "contains path or query characters (/, ?, #)")
	case strings.ContainsAny(value, shellMetaChars):
		return newError(field, RuleMetachars, "contains shell metacharacters")
	case len(value) > 128:
		return newError(field, RuleRange, "must be 128 characters or fewer")
	case !podNameRegexp.MatchString(value):
		return newError(field, RuleFormat, "use letters, digits, hyphens, and underscores only")
	}
	return nil
}

// containerNameRegexp mirrors Docker's own container-name rule: the first character
// must be alphanumeric, the rest may also include underscores, dots, and hyphens.
// The daemon rejects anything else with "Invalid container name", so accepting it
// here would only defer the failure to container creation.
var containerNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// ContainerName validates a user-supplied Docker container name. Like PodName it runs
// ordered deny-checks so the most specific reason wins, then the allow-list. The
// 128-character cap is a local sanity limit against absurd inputs, not a Docker rule.
func ContainerName(value string) error {
	const field = "container name"
	switch {
	case value == "":
		return newError(field, RuleEmpty, "must not be empty")
	case containsControlChars(value):
		return newError(field, RuleControlChars, "contains control characters")
	case strings.Contains(value, "%"):
		return newError(field, RuleEncoding, "contains percent-encoding (pass the decoded value)")
	case strings.ContainsAny(value, "/?#"):
		return newError(field, RuleEmbedded, "contains path or query characters (/, ?, #)")
	case strings.ContainsAny(value, shellMetaChars):
		return newError(field, RuleMetachars, "contains shell metacharacters")
	case len(value) > 128:
		return newError(field, RuleRange, "must be 128 characters or fewer")
	case !containerNameRegexp.MatchString(value):
		return newError(field, RuleFormat, "must start with a letter or digit and use only letters, digits, dots, hyphens, and underscores")
	}
	return nil
}

// awsAccountIDRegexp matches an AWS account id: exactly 12 decimal digits. The
// rule mirrors how LocalStack derives the account from the access key id it
// receives — a 12-digit key selects that account, anything else falls back to
// the default 000000000000 — rather than any AWS API contract.
var awsAccountIDRegexp = regexp.MustCompile(`^\d{12}$`)

// AWSAccountID validates a user-supplied AWS account id. Like PodName it runs
// ordered deny-checks so the most specific reason wins: a wrong-length value is
// a range failure, a right-length value with non-digits is a format failure.
func AWSAccountID(value string) error {
	const field = "account"
	switch {
	case value == "":
		return newError(field, RuleEmpty, "must not be empty")
	case containsControlChars(value):
		return newError(field, RuleControlChars, "contains control characters")
	case len(value) != 12:
		return newError(field, RuleRange, "must be exactly 12 digits")
	case !awsAccountIDRegexp.MatchString(value):
		return newError(field, RuleFormat, "must contain digits only")
	}
	return nil
}

// serviceListRegexp matches a comma-delimited list of service-name tokens:
// letters, digits, underscores, and hyphens, with optional whitespace around
// each item and separator. Mirrors the legacy CLI's is_comma_delimited_list
// check. This is syntactic only — lstk has no canonical service registry to
// validate membership against, and the platform silently drops unrecognized
// names rather than rejecting them, so a stricter client-side allow-list
// would only diverge from server behavior.
var serviceListRegexp = regexp.MustCompile(`^\s*[\w-]+(\s*,\s*[\w-]+)*\s*$`)

// ServiceList validates and parses a comma-separated --services value. An
// empty string means "no filter" and returns (nil, nil) — equivalent to
// omitting the flag. Each item is trimmed of surrounding whitespace;
// duplicates are preserved (not deduplicated), matching the legacy CLI.
func ServiceList(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	if !serviceListRegexp.MatchString(value) {
		return nil, newError("services", RuleFormat, "must be a comma-separated list of service names (letters, digits, hyphens, underscores)")
	}
	parts := strings.Split(value, ",")
	services := make([]string, len(parts))
	for i, p := range parts {
		services[i] = strings.TrimSpace(p)
	}
	return services, nil
}

// AuthToken validates a LocalStack auth token. The character set is intentionally
// not restricted — tokens are opaque — so only clearly malformed values are
// rejected: control characters, embedded whitespace, or an implausible length. An
// empty token is allowed (it means none is set). Callers should TrimSpace first,
// since environment injection (e.g. CI secrets) commonly appends a trailing newline.
func AuthToken(value string) error {
	if value == "" {
		return nil
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return newError("auth token", RuleControlChars, "contains control characters")
		}
		if unicode.IsSpace(r) {
			return newError("auth token", RuleFormat, "contains whitespace")
		}
	}
	if len(value) > 1024 {
		return newError("auth token", RuleRange, "is implausibly long (over 1024 characters)")
	}
	return nil
}
