package env

import (
	"os"
	"strings"
	"testing"
)

type Key string

const (
	AuthToken   Key = "LSTK_AUTH_TOKEN"
	APIEndpoint Key = "LSTK_API_ENDPOINT"
	Keyring     Key = "LSTK_KEYRING"
	CI          Key = "CI"
)

func Get(key Key) string {
	return os.Getenv(string(key))
}

func Require(t testing.TB, key Key) string {
	t.Helper()
	v := os.Getenv(string(key))
	if v == "" {
		t.Fatalf("%s must be set to run this test", key)
	}
	return v
}

type Environ []string

func Without(keys ...Key) Environ {
	return Environ(os.Environ()).Without(keys...)
}

func With(key Key, value string) Environ {
	return Environ(os.Environ()).With(key, value)
}

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

func (e Environ) With(key Key, value string) Environ {
	return append(e, string(key)+"="+value)
}
