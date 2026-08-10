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

func TestContainerName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    string
		wantErr  bool
		wantRule string
	}{
		{"default derived name", "localstack-aws", false, ""},
		{"derived name with tag", "localstack-aws-2026.4", false, ""},
		{"single char", "a", false, ""},
		{"digit start", "1st-emulator", false, ""},
		{"underscore", "ls_jenkins", false, ""},
		{"period", "ls.jenkins", false, ""},
		{"maximum length", strings.Repeat("a", 128), false, ""},
		{"too long", strings.Repeat("a", 129), true, RuleRange},
		{"empty", "", true, RuleEmpty},
		{"control char", "ba\x00d", true, RuleControlChars},
		{"percent encoding", "ls%2Fmain", true, RuleEncoding},
		{"slash", "team/ls", true, RuleEmbedded},
		{"path traversal", "../etc", true, RuleEmbedded},
		{"embedded query", "ls?fields=name", true, RuleEmbedded},
		{"fragment", "ls#frag", true, RuleEmbedded},
		{"shell metachar semicolon", "ls;rm", true, RuleMetachars},
		{"shell metachar subshell", "ls$(id)", true, RuleMetachars},
		{"shell metachar backtick", "ls`id`", true, RuleMetachars},
		{"leading hyphen", "-ls", true, RuleFormat},
		{"leading underscore", "_ls", true, RuleFormat},
		{"leading dot", ".ls", true, RuleFormat},
		{"space", "my emulator", true, RuleFormat},
		{"colon", "ls:1", true, RuleFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ContainerName(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ContainerName(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if tt.wantRule != "" {
				var ve *Error
				if !errors.As(err, &ve) {
					t.Fatalf("ContainerName(%q) error is not *validate.Error: %v", tt.value, err)
				}
				if ve.Rule != tt.wantRule {
					t.Errorf("ContainerName(%q) Rule = %q, want %q", tt.value, ve.Rule, tt.wantRule)
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

func TestAWSAccountID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    string
		wantErr  bool
		wantRule string
	}{
		{"valid", "111111111111", false, ""},
		{"default account", "000000000000", false, ""},
		{"empty", "", true, RuleEmpty},
		{"control char", "1111111\x0011111", true, RuleControlChars},
		{"too short", "11111111111", true, RuleRange},
		{"too long", "1111111111111", true, RuleRange},
		{"single digit", "1", true, RuleRange},
		{"right length with letter", "12345678901a", true, RuleFormat},
		{"real access key", "AKIAIOSFODNN", true, RuleFormat},
		{"right length with hyphen", "1234-5678901", true, RuleFormat},
		{"right length with space", "12345678901 ", true, RuleFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := AWSAccountID(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("AWSAccountID(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if tt.wantRule != "" {
				var ve *Error
				if !errors.As(err, &ve) {
					t.Fatalf("AWSAccountID(%q) error is not *Error: %v", tt.value, err)
				}
				if ve.Rule != tt.wantRule {
					t.Errorf("AWSAccountID(%q) rule = %q, want %q", tt.value, ve.Rule, tt.wantRule)
				}
			}
		})
	}
}

func TestServiceList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    string
		want     []string
		wantErr  bool
		wantRule string
	}{
		{"empty means no filter", "", nil, false, ""},
		{"single value", "s3", []string{"s3"}, false, ""},
		{"multiple values", "s3,lambda,dynamodb", []string{"s3", "lambda", "dynamodb"}, false, ""},
		{"whitespace around items trimmed", "s3, lambda , dynamodb", []string{"s3", "lambda", "dynamodb"}, false, ""},
		{"underscores and hyphens allowed", "cognito-idp,step_functions", []string{"cognito-idp", "step_functions"}, false, ""},
		{"duplicates preserved", "s3,s3,lambda", []string{"s3", "s3", "lambda"}, false, ""},
		{"whitespace only", "   ", nil, true, RuleFormat},
		{"double comma", "s3,,lambda", nil, true, RuleFormat},
		{"trailing comma", "s3,", nil, true, RuleFormat},
		{"leading comma", ",s3", nil, true, RuleFormat},
		{"semicolon separated", "s3;lambda", nil, true, RuleFormat},
		{"embedded space in item", "s3, la mbda", nil, true, RuleFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ServiceList(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ServiceList(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Fatalf("ServiceList(%q) = %v, want %v", tt.value, got, tt.want)
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Fatalf("ServiceList(%q) = %v, want %v", tt.value, got, tt.want)
					}
				}
			}
			if tt.wantRule != "" {
				var ve *Error
				if !errors.As(err, &ve) {
					t.Fatalf("ServiceList(%q) error is not *validate.Error: %v", tt.value, err)
				}
				if ve.Rule != tt.wantRule {
					t.Errorf("ServiceList(%q) Rule = %q, want %q", tt.value, ve.Rule, tt.wantRule)
				}
			}
		})
	}
}
