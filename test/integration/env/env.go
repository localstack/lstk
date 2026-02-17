package env

import (
	"os"
	"strings"
	"testing"
)

// Key is a declared environment variable name.
type Key string

const (
	AuthToken   Key = "LOCALSTACK_AUTH_TOKEN"
	APIEndpoint Key = "LOCALSTACK_API_ENDPOINT"
	Keyring     Key = "LOCALSTACK_KEYRING"
	CI          Key = "CI"
)

// Get returns the value of the given environment variable.
func Get(key Key) string {
	return os.Getenv(string(key))
}

// Require returns the value of the given environment variable, failing the test if it is not set.
func Require(t testing.TB, key Key) string {
	t.Helper()
	v := os.Getenv(string(key))
	if v == "" {
		t.Fatalf("%s must be set to run this test", key)
	}
	return v
}

// Environ is a slice of "KEY=value" environment variable strings.
type Environ []string

// Without returns the current process environment excluding the given keys.
func Without(keys ...Key) Environ {
	return Environ(os.Environ()).Without(keys...)
}

// With returns the current process environment with key=value appended.
func With(key Key, value string) Environ {
	return Environ(os.Environ()).With(key, value)
}

// Without returns a copy of e excluding any variable whose key matches one of the given keys.
func (e Environ) Without(keys ...Key) Environ {
	var result Environ
	for _, entry := range e {
		excluded := false
		for _, key := range keys {
			if strings.HasPrefix(entry, string(key)+"=") {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, entry)
		}
	}
	return result
}

// With returns a copy of e with key=value appended.
func (e Environ) With(key Key, value string) Environ {
	return append(e, string(key)+"="+value)
}
