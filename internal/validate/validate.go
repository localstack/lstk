package validate

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// containsControlChars checks for control characters (0x00-0x1F, 0x7F)
// excluding tab, newline, carriage return.
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

// NoControlChars rejects strings containing control characters.
func NoControlChars(field, value string) error {
	if containsControlChars(value) {
		return fmt.Errorf("%s contains invalid control characters", field)
	}
	return nil
}

// Port validates a port string is a number in 1-65535.
func Port(value string) error {
	if err := NoControlChars("port", value); err != nil {
		return err
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("port %q is not a valid number", value)
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("port %d is out of range (1-65535)", n)
	}
	return nil
}

var dockerTagRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// DockerTag validates a Docker image tag.
func DockerTag(value string) error {
	if value == "" {
		return nil
	}
	if !dockerTagRegexp.MatchString(value) {
		return fmt.Errorf("tag %q contains invalid characters (allowed: alphanumeric, dots, hyphens, underscores)", value)
	}
	return nil
}

// URLPathSegment rejects path traversals and embedded query params in a URL path segment.
func URLPathSegment(field, value string) error {
	if err := NoControlChars(field, value); err != nil {
		return err
	}
	if strings.Contains(value, "..") {
		return fmt.Errorf("%s contains path traversal", field)
	}
	if strings.ContainsAny(value, "/?#") {
		return fmt.Errorf("%s contains invalid characters for a path segment", field)
	}
	return nil
}

// HTTPSURL validates a URL is well-formed with an http or https scheme.
func HTTPSURL(field, value string) error {
	if err := NoControlChars(field, value); err != nil {
		return err
	}
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", field, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s must use http or https scheme, got %q", field, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%s is missing a host", field)
	}
	return nil
}

var envVarKeyRegexp = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// EnvVar validates a KEY=VALUE environment variable string.
func EnvVar(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("env %q must be in KEY=VALUE format", value)
	}
	key := parts[0]
	if !envVarKeyRegexp.MatchString(key) {
		return fmt.Errorf("env key %q contains invalid characters", key)
	}
	if containsControlChars(parts[1]) {
		return fmt.Errorf("env value for %q contains control characters", key)
	}
	return nil
}
