package endpoint

import "testing"

func TestDeriveS3Endpoint(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"localhost with port", "http://localhost:4566", "http://s3.localhost.localstack.cloud:4566"},
		{"127.0.0.1 with port", "http://127.0.0.1:4566", "http://s3.localhost.localstack.cloud:4566"},
		{"named host with port", "http://my.localstack.example:4566", "http://s3.my.localstack.example:4566"},
		{"named host without port", "https://gw.example", "https://s3.gw.example"},
		{"wildcard host already in use", "http://localhost.localstack.cloud:4566", "http://s3.localhost.localstack.cloud:4566"},
		{"empty input", "", ""},
		{"missing host", "http://", ""},
		{"scheme defaults to http when absent", "//localhost:4566", "http://s3.localhost.localstack.cloud:4566"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveS3Endpoint(tc.in)
			if got != tc.want {
				t.Fatalf("DeriveS3Endpoint(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}
