package awsconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
			ok, err := ProfileExists(context.Background())
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
				ok, err := ProfileExists(context.Background())
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

func TestWriteConfigProfileWritesS3ServicesOverride(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".aws", "config")

	if err := writeConfigProfile(configPath, "localhost.localstack.cloud:4566"); err != nil {
		t.Fatal(err)
	}

	// Key order is not guaranteed since upsertSection ranges over a map, so
	// check parsed values instead of raw file text.
	f, err := loadINI(configPath)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := f.GetSection(configSectionName)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"region":       "us-east-1",
		"output":       "json",
		"endpoint_url": "http://localhost.localstack.cloud:4566",
		"services":     "localstack",
	} {
		if got := profile.Key(key).Value(); got != want {
			t.Errorf("profile key %q = %q, want %q", key, got, want)
		}
	}

	services, err := f.GetSection(servicesSectionName)
	if err != nil {
		t.Fatal(err)
	}
	// Body() strips the continuation line's leading whitespace on load, so check
	// the on-disk bytes instead to catch a regression that drops the indentation.
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	wantServicesBlock := "[services localstack]\n" +
		"s3 =\n" +
		"    endpoint_url = http://s3.localhost.localstack.cloud:4566\n"
	if !strings.Contains(string(raw), wantServicesBlock) {
		t.Errorf("config content missing indented services block\ngot:\n%s\nwant substring:\n%s", raw, wantServicesBlock)
	}
	if got := services.Body(); got != "s3 =\nendpoint_url = http://s3.localhost.localstack.cloud:4566" {
		t.Errorf("services section body = %q", got)
	}

	needed, err := configNeedsWrite(configPath, "localhost.localstack.cloud:4566")
	if err != nil {
		t.Fatal(err)
	}
	if needed {
		t.Error("config should not need a write immediately after writeConfigProfile")
	}
}

// A profile update rewrites the whole ~/.aws/config. Nested blocks in other
// sections — a profile's s3 settings, or another [services …] endpoint — must
// stay indented, or the AWS CLI stops applying them.
func TestWriteConfigProfilePreservesForeignNestedSections(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".aws", "config")
	writeFile(t, configPath, strings.Join([]string{
		"[default]",
		"region = us-east-1",
		"s3 =",
		"    max_concurrent_requests = 20",
		"",
		"[profile dev]",
		"region = us-west-2",
		"services = dev",
		"",
		"[services dev]",
		"s3 =",
		"    endpoint_url = https://s3.private.example",
		"    addressing_style = path",
		"",
		"[profile localstack]",
		"region = us-east-1",
		"output = json",
		"endpoint_url = http://localhost.localstack.cloud:4566",
		"",
	}, "\n"))

	if err := writeConfigProfile(configPath, "localhost.localstack.cloud:4566"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	assertNested(t, text, "default", "s3", "max_concurrent_requests = 20")
	assertNested(t, text, "services dev", "s3",
		"endpoint_url = https://s3.private.example",
		"addressing_style = path",
	)
	assertNested(t, text, "services localstack", "s3",
		"endpoint_url = http://s3.localhost.localstack.cloud:4566",
	)

	needed, err := configNeedsWrite(configPath, "localhost.localstack.cloud:4566")
	if err != nil {
		t.Fatal(err)
	}
	if needed {
		t.Error("config should not need a write after preserving foreign sections")
	}
}

// assertNested reports when child lines are not indented under parent in section.
func assertNested(t *testing.T, text, section, parent string, children ...string) {
	t.Helper()
	marker := "[" + section + "]"
	idx := strings.Index(text, marker)
	if idx < 0 {
		t.Fatalf("missing section %q\n%s", section, text)
	}
	rest := text[idx+len(marker):]
	if next := strings.Index(rest, "\n["); next >= 0 {
		rest = rest[:next]
	}
	lines := strings.Split(rest, "\n")
	parentAt := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == parent || strings.HasPrefix(trimmed, parent+" ") || strings.HasPrefix(trimmed, parent+"=") {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				continue
			}
			parentAt = i
			break
		}
	}
	if parentAt < 0 {
		t.Fatalf("section %q missing %q\n%s", section, parent, text)
	}
	for _, child := range children {
		found := false
		for _, line := range lines[parentAt+1:] {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				break
			}
			if strings.TrimSpace(line) == child || strings.HasPrefix(strings.TrimSpace(line), child) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("section %q: %q is not nested under %s\n%s", section, child, parent, text)
		}
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
			name: "valid profile needs nothing",
			configContent: "[profile localstack]\nregion = us-east-1\noutput = json\n" +
				"endpoint_url = http://localhost.localstack.cloud:4566\nservices = localstack\n\n" +
				"[services localstack]\ns3 =\n    endpoint_url = http://s3.localhost.localstack.cloud:4566\n",
			credsContent: "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
			resolvedHost: "localhost.localstack.cloud:4566",
			wantConfig:   false,
			wantCreds:    false,
		},
		{
			name:          "missing services key needs write",
			configContent: "[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://localhost.localstack.cloud:4566\n",
			credsContent:  "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
			resolvedHost:  "localhost.localstack.cloud:4566",
			wantConfig:    true,
			wantCreds:     false,
		},
		{
			name: "missing services section needs write",
			configContent: "[profile localstack]\nregion = us-east-1\noutput = json\n" +
				"endpoint_url = http://localhost.localstack.cloud:4566\nservices = localstack\n",
			credsContent: "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
			resolvedHost: "localhost.localstack.cloud:4566",
			wantConfig:   true,
			wantCreds:    false,
		},
		{
			name: "stale s3 endpoint in services section needs write",
			configContent: "[profile localstack]\nregion = us-east-1\noutput = json\n" +
				"endpoint_url = http://localhost.localstack.cloud:4566\nservices = localstack\n\n" +
				"[services localstack]\ns3 =\n    endpoint_url = http://s3.some-other-host:4566\n",
			credsContent: "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
			resolvedHost: "localhost.localstack.cloud:4566",
			wantConfig:   true,
			wantCreds:    false,
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
			name: "wrong credentials",
			configContent: "[profile localstack]\nregion = us-east-1\noutput = json\n" +
				"endpoint_url = http://127.0.0.1:4566\nservices = localstack\n\n" +
				"[services localstack]\ns3 =\n    endpoint_url = http://127.0.0.1:4566\n",
			credsContent: "[localstack]\naws_access_key_id = wrong\naws_secret_access_key = wrong\n",
			resolvedHost: "127.0.0.1:4566",
			wantConfig:   false,
			wantCreds:    true,
		},
		{
			name: "127.0.0.1 profile valid when DNS now resolves to localhost.localstack.cloud",
			configContent: "[profile localstack]\nregion = us-east-1\noutput = json\n" +
				"endpoint_url = http://127.0.0.1:4566\nservices = localstack\n\n" +
				"[services localstack]\ns3 =\n    endpoint_url = http://s3.localhost.localstack.cloud:4566\n",
			credsContent: "[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n",
			resolvedHost: "localhost.localstack.cloud:4566",
			wantConfig:   false,
			wantCreds:    false,
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
