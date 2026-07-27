package validate

import (
	"errors"
	"strings"
	"testing"
)

func TestNoControlChars(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			err := NoControlChars("test", tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("NoControlChars() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPodName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    string
		wantErr  bool
		wantRule string
	}{
		{"simple", "my-baseline", false, ""},
		{"alphanumeric", "abc123", false, ""},
		{"single char", "a", false, ""},
		{"long hyphenated", "my-long-pod-name-123", false, ""},
		{"leading underscore", "_baseline", false, ""},
		{"leading hyphen", "-baseline", false, ""},
		{"maximum length", strings.Repeat("a", 128), false, ""},
		{"too long", strings.Repeat("a", 129), true, RuleRange},
		{"empty", "", true, RuleEmpty},
		{"control char", "ba\x00d", true, RuleControlChars},
		{"percent encoding", "staging%2Fpod", true, RuleEncoding},
		{"path traversal", "../etc", true, RuleEmbedded},
		{"period", "release.v1", true, RuleFormat},
		{"consecutive periods", "release..v1", true, RuleFormat},
		{"embedded query", "abc?fields=name", true, RuleEmbedded},
		{"slash", "a/b", true, RuleEmbedded},
		{"fragment", "id#frag", true, RuleEmbedded},
		{"shell metachar semicolon", "a;rm", true, RuleMetachars},
		{"shell metachar subshell", "a$(id)", true, RuleMetachars},
		{"shell metachar backtick", "a`id`", true, RuleMetachars},
		{"underscore", "my_pod", false, ""},
		{"leading dot", ".hidden", true, RuleFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := PodName(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("PodName(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if tt.wantRule != "" {
				var ve *Error
				if !errors.As(err, &ve) {
					t.Fatalf("PodName(%q) error is not *validate.Error: %v", tt.value, err)
				}
				if ve.Rule != tt.wantRule {
					t.Errorf("PodName(%q) Rule = %q, want %q", tt.value, ve.Rule, tt.wantRule)
				}
			}
		})
	}
}

func TestAuthToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty is allowed", "", false},
		{"typical token", "ls-example-token", false},
		{"alphanumeric", "exampletoken123", false},
		{"with null byte", "tok\x00en", true},
		{"with escape", "tok\x1ben", true},
		{"with newline", "token\n", true},
		{"with tab", "tok\ten", true},
		{"with space", "tok en", true},
		{"too long", strings.Repeat("a", 1025), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := AuthToken(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("AuthToken(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}
