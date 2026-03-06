package validate

import (
	"testing"
)

func TestNoControlChars(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"clean string", "hello world", false},
		{"with tab", "hello\tworld", false},
		{"with newline", "hello\nworld", false},
		{"with null byte", "hello\x00world", true},
		{"with bell", "hello\x07world", true},
		{"with escape", "hello\x1bworld", true},
		{"with delete", "hello\x7fworld", true},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NoControlChars("test", tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("NoControlChars() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPort(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid port", "4566", false},
		{"min port", "1", false},
		{"max port", "65535", false},
		{"zero", "0", true},
		{"negative", "-1", true},
		{"too high", "65536", true},
		{"not a number", "abc", true},
		{"with space", "45 66", true},
		{"empty", "", true},
		{"control char", "45\x0066", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Port(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("Port(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestDockerTag(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"latest", "latest", false},
		{"version", "3.7.2", false},
		{"with hyphen", "my-tag", false},
		{"with underscore", "my_tag", false},
		{"empty", "", false},
		{"path traversal", "../evil", true},
		{"with slash", "evil/tag", true},
		{"with colon", "tag:sub", true},
		{"with space", "tag name", true},
		{"starts with dot", ".hidden", true},
		{"starts with hyphen", "-bad", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DockerTag(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("DockerTag(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestURLPathSegment(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"simple id", "abc123", false},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"path traversal", "../etc/passwd", true},
		{"with query", "id?extra=1", true},
		{"with slash", "a/b", true},
		{"with fragment", "id#frag", true},
		{"with null", "id\x00", true},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := URLPathSegment("test", tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("URLPathSegment(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestHTTPSURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"https url", "https://api.localstack.cloud", false},
		{"http url", "http://localhost:4566", false},
		{"ftp scheme", "ftp://evil.com", true},
		{"no scheme", "api.localstack.cloud", true},
		{"file scheme", "file:///etc/passwd", true},
		{"javascript", "javascript:alert(1)", true},
		{"with control char", "https://evil\x00.com", true},
		{"empty host", "https://", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := HTTPSURL("test", tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("HTTPSURL(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestEnvVar(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid", "MY_VAR=value", false},
		{"empty value", "MY_VAR=", false},
		{"value with equals", "MY_VAR=a=b", false},
		{"no equals", "MY_VAR", true},
		{"bad key chars", "MY-VAR=val", true},
		{"key starts with number", "1VAR=val", true},
		{"control in value", "MY_VAR=val\x00ue", true},
		{"control in key", "MY\x00VAR=value", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnvVar(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnvVar(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}
