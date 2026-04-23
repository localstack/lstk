package awsconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestProfileExists(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{
			name: "both present",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, ".aws", "config"), "[profile localstack]\nregion = us-east-1\n")
				writeFile(t, filepath.Join(dir, ".aws", "credentials"), "[localstack]\naws_access_key_id = test\n")
			},
			want: true,
		},
		{
			name: "config missing",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, ".aws", "credentials"), "[localstack]\naws_access_key_id = test\n")
			},
			want: false,
		},
		{
			name: "credentials missing",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, ".aws", "config"), "[profile localstack]\nregion = us-east-1\n")
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			tc.setup(t, dir)
			ok, err := ProfileExists()
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.want {
				t.Errorf("got %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestWriteProfile(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		check func(t *testing.T, dir string)
	}{
		{
			name:  "creates files when absent",
			setup: func(t *testing.T, dir string) {},
			check: func(t *testing.T, dir string) {
				ok, err := ProfileExists()
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					t.Error("expected profile to exist after writeProfile")
				}
			},
		},
		{
			name: "preserves existing profiles",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, ".aws", "config"), "[profile default]\nregion = eu-west-1\n")
			},
			check: func(t *testing.T, dir string) {
				ok, err := sectionExists(filepath.Join(dir, ".aws", "config"), "profile default")
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					t.Error("existing profile was lost after writeProfile")
				}
			},
		},
		{
			name: "updates stale localstack section",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, ".aws", "config"), "[profile localstack]\nregion = eu-west-1\nendpoint_url = http://wrong.host:1234\n")
				writeFile(t, filepath.Join(dir, ".aws", "credentials"), "[localstack]\naws_access_key_id = old\naws_secret_access_key = old\n")
			},
			check: func(t *testing.T, dir string) {
				configNeeded, err := configNeedsWrite(filepath.Join(dir, ".aws", "config"), "localhost.localstack.cloud:4566")
				if err != nil {
					t.Fatal(err)
				}
				if configNeeded {
					t.Error("config should not need a write after writeProfile")
				}
				credsNeeded, err := credsNeedWrite(filepath.Join(dir, ".aws", "credentials"))
				if err != nil {
					t.Fatal(err)
				}
				if credsNeeded {
					t.Error("credentials should not need a write after writeProfile")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			tc.setup(t, dir)
			if err := writeProfile("localhost.localstack.cloud:4566"); err != nil {
				t.Fatal(err)
			}
			tc.check(t, dir)
		})
	}
}

func TestCheckProfileStatus(t *testing.T) {
	tests := []struct {
		name          string
		configContent string
		credsContent  string
		resolvedHost  string
		wantConfig    bool
		wantCreds     bool
	}{
		{
			name:         "both files missing",
			resolvedHost: "localhost.localstack.cloud:4566",
			wantConfig:   true,
			wantCreds:    true,
		},
		{
			name:          "valid profile needs nothing",
			configContent: "[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://localhost.localstack.cloud:4566\n",
			credsContent:  "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
			resolvedHost:  "localhost.localstack.cloud:4566",
			wantConfig:    false,
			wantCreds:     false,
		},
		{
			name:          "missing endpoint_url",
			configContent: "[profile localstack]\nregion = us-east-1\n",
			credsContent:  "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
			resolvedHost:  "localhost.localstack.cloud:4566",
			wantConfig:    true,
			wantCreds:     false,
		},
		{
			name:          "invalid endpoint_url",
			configContent: "[profile localstack]\nregion = us-east-1\nendpoint_url = http://some-other-host:4566\n",
			credsContent:  "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
			resolvedHost:  "localhost.localstack.cloud:4566",
			wantConfig:    true,
			wantCreds:     false,
		},
		{
			name:          "wrong credentials",
			configContent: "[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://127.0.0.1:4566\n",
			credsContent:  "[localstack]\naws_access_key_id = wrong\naws_secret_access_key = wrong\n",
			resolvedHost:  "127.0.0.1:4566",
			wantConfig:    false,
			wantCreds:     true,
		},
		{
			name:          "127.0.0.1 profile valid when DNS now resolves to localhost.localstack.cloud",
			configContent: "[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://127.0.0.1:4566\n",
			credsContent:  "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
			resolvedHost:  "localhost.localstack.cloud:4566",
			wantConfig:    false,
			wantCreds:     false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, ".aws", "config")
			credsPath := filepath.Join(dir, ".aws", "credentials")
			if tc.configContent != "" {
				writeFile(t, configPath, tc.configContent)
			}
			if tc.credsContent != "" {
				writeFile(t, credsPath, tc.credsContent)
			}
			t.Setenv("HOME", dir)
			status, err := CheckProfileStatus(tc.resolvedHost)
			if err != nil {
				t.Fatal(err)
			}
			if status.configNeeded != tc.wantConfig {
				t.Errorf("configNeeded: got %v, want %v", status.configNeeded, tc.wantConfig)
			}
			if status.credsNeeded != tc.wantCreds {
				t.Errorf("credsNeeded: got %v, want %v", status.credsNeeded, tc.wantCreds)
			}
		})
	}
}

func TestCheckProfileStatusMalformedFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".aws", "config")
	credsPath := filepath.Join(dir, ".aws", "credentials")

	writeFile(t, configPath, "this is not valid \x00\x01\x02 ini content [[[")
	writeFile(t, credsPath, "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n")

	// Override HOME to use our test directory
	t.Setenv("HOME", dir)
	_, err := CheckProfileStatus("127.0.0.1:4566")
	if err == nil {
		t.Error("expected error for malformed config file, got nil")
	}
}

func TestIsValidLocalStackEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		endpointURL  string
		resolvedHost string
		want         bool
	}{
		{
			name:         "valid http",
			endpointURL:  "http://localhost.localstack.cloud:4566",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         true,
		},
		{
			name:         "valid https",
			endpointURL:  "https://localhost.localstack.cloud:4566",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         true,
		},
		{
			name:         "valid fallback ip",
			endpointURL:  "http://127.0.0.1:4566",
			resolvedHost: "127.0.0.1:4566",
			want:         true,
		},
		{
			name:         "wrong host",
			endpointURL:  "http://some-other-host:4566",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         false,
		},
		{
			name:         "wrong port",
			endpointURL:  "http://localhost.localstack.cloud:9999",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         false,
		},
		{
			name:         "missing port",
			endpointURL:  "http://localhost.localstack.cloud",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         false,
		},
		{
			name:         "trailing slash",
			endpointURL:  "http://localhost.localstack.cloud:4566/",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         true, // trailing slash is functionally equivalent; host still matches
		},
		{
			name:         "unsupported scheme",
			endpointURL:  "ftp://localhost.localstack.cloud:4566",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         false,
		},
		{
			name:         "unparseable url",
			endpointURL:  "://bad-url",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         false,
		},
		{
			name:         "empty string",
			endpointURL:  "",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         false,
		},
		{
			name:         "127.0.0.1 accepted when resolved host is localhost.localstack.cloud",
			endpointURL:  "http://127.0.0.1:4566",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         true,
		},
		{
			name:         "localhost.localstack.cloud accepted when resolved host is 127.0.0.1",
			endpointURL:  "http://localhost.localstack.cloud:4566",
			resolvedHost: "127.0.0.1:4566",
			want:         true,
		},
		{
			name:         "127.0.0.1 with wrong port rejected",
			endpointURL:  "http://127.0.0.1:9999",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         false,
		},
		{
			name:         "localhost accepted when resolved host is localhost.localstack.cloud",
			endpointURL:  "http://localhost:4566",
			resolvedHost: "localhost.localstack.cloud:4566",
			want:         true,
		},
		{
			name:         "localhost accepted when resolved host is 127.0.0.1",
			endpointURL:  "http://localhost:4566",
			resolvedHost: "127.0.0.1:4566",
			want:         true,
		},
		{
			name:         "custom host not interchangeable with 127.0.0.1",
			endpointURL:  "http://127.0.0.1:4566",
			resolvedHost: "myhost.internal:4566",
			want:         false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isValidLocalStackEndpoint(tc.endpointURL, tc.resolvedHost)
			if got != tc.want {
				t.Errorf("isValidLocalStackEndpoint(%q, %q) = %v, want %v", tc.endpointURL, tc.resolvedHost, got, tc.want)
			}
		})
	}
}

