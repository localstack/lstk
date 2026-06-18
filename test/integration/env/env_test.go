package env

import (
	"strings"
	"testing"
)

// resolve returns the effective value of key in e, mirroring how os/exec
// dedups duplicate keys: the last occurrence wins.
func resolve(e Environ, key Key) (string, bool) {
	value, found := "", false
	prefix := string(key) + "="
	for _, entry := range e {
		if strings.HasPrefix(entry, prefix) {
			value, found = strings.TrimPrefix(entry, prefix), true
		}
	}
	return value, found
}

// Guards against telemetry from integration tests reaching the production
// analytics backend: the base env helpers must default the analytics endpoint
// to an unreachable address unless a test explicitly overrides it.
func TestBaseEnvDefaultsAnalyticsEndpointToUnreachable(t *testing.T) {
	cases := map[string]Environ{
		"With":    With("SOME_VAR", "value"),
		"Without": Without(AuthToken),
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			got, found := resolve(e, AnalyticsEndpoint)
			if !found {
				t.Fatalf("%s did not set %s", name, AnalyticsEndpoint)
			}
			if got != UnreachableAnalyticsEndpoint {
				t.Fatalf("%s analytics endpoint = %q, want %q", name, got, UnreachableAnalyticsEndpoint)
			}
		})
	}
}

func TestExplicitAnalyticsEndpointOverridesDefault(t *testing.T) {
	const mock = "http://127.0.0.1:54321"
	e := With(APIEndpoint, "http://example").With(AnalyticsEndpoint, mock)
	got, _ := resolve(e, AnalyticsEndpoint)
	if got != mock {
		t.Fatalf("analytics endpoint = %q, want %q (explicit override must win over default)", got, mock)
	}
}
