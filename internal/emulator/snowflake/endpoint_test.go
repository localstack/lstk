package snowflake

import "testing"

func TestEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		resolvedHost string
		want         string
	}{
		{"hostname with port", "localhost.localstack.cloud:4566", "http://snowflake.localhost.localstack.cloud:4566"},
		{"custom port", "localhost.localstack.cloud:4567", "http://snowflake.localhost.localstack.cloud:4567"},
		{"ipv4 host", "127.0.0.1:4566", ""},
		{"ipv6 host", "[::1]:4566", ""},
		{"missing port", "localhost.localstack.cloud", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Endpoint(tt.resolvedHost); got != tt.want {
				t.Errorf("Endpoint(%q) = %q, want %q", tt.resolvedHost, got, tt.want)
			}
		})
	}
}
