package cmd

import (
	"reflect"
	"testing"
)

func TestStripLeadingTerraformFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantRemain  []string
		wantRegion  string
		wantAccount string
		wantChdir   string
		wantErr     bool
	}{
		{
			name:       "no flags",
			args:       []string{"plan"},
			wantRemain: []string{"plan"},
		},
		{
			name:       "region space form",
			args:       []string{"--region", "us-west-2", "plan"},
			wantRemain: []string{"plan"},
			wantRegion: "us-west-2",
		},
		{
			name:       "region equals form",
			args:       []string{"--region=us-west-2", "plan"},
			wantRemain: []string{"plan"},
			wantRegion: "us-west-2",
		},
		{
			name:        "both flags both forms",
			args:        []string{"--region", "eu-west-1", "--account=111111111111", "apply", "-auto-approve"},
			wantRemain:  []string{"apply", "-auto-approve"},
			wantRegion:  "eu-west-1",
			wantAccount: "111111111111",
		},
		{
			name:       "flags after action are forwarded verbatim",
			args:       []string{"plan", "--region", "us-west-2"},
			wantRemain: []string{"plan", "--region", "us-west-2"},
		},
		{
			name:    "region missing value",
			args:    []string{"--region"},
			wantErr: true,
		},
		{
			name:    "account missing value",
			args:    []string{"--region", "us-east-1", "--account"},
			wantErr: true,
		},
		{
			name:       "stops at first non-flag token",
			args:       []string{"--account=111111111111", "apply", "--region", "x"},
			wantRemain: []string{"apply", "--region", "x"},
			// region stays empty because --region appears after the action
			wantAccount: "111111111111",
		},
		{
			name:       "chdir is read and kept in forwarded args",
			args:       []string{"-chdir=infra", "plan"},
			wantRemain: []string{"-chdir=infra", "plan"},
			wantChdir:  "infra",
		},
		{
			name:        "chdir before leading flags keeps chdir and consumes flags",
			args:        []string{"-chdir=infra", "--region", "us-west-2", "--account=111111111111", "apply"},
			wantRemain:  []string{"-chdir=infra", "apply"},
			wantRegion:  "us-west-2",
			wantAccount: "111111111111",
			wantChdir:   "infra",
		},
		{
			name:       "chdir after leading flags keeps chdir and consumes flags",
			args:       []string{"--region", "us-west-2", "-chdir=infra", "plan"},
			wantRemain: []string{"-chdir=infra", "plan"},
			wantRegion: "us-west-2",
			wantChdir:  "infra",
		},
		{
			name:       "space-separated chdir form is not interpreted and is forwarded",
			args:       []string{"-chdir", "infra", "plan"},
			wantRemain: []string{"-chdir", "infra", "plan"},
			// scanning stops at the unrecognized -chdir token; nothing consumed.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remain, region, account, chdir, err := stripLeadingIaCFlags(tt.args, true)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(remain, tt.wantRemain) {
				t.Errorf("remaining = %v, want %v", remain, tt.wantRemain)
			}
			if region != tt.wantRegion {
				t.Errorf("region = %q, want %q", region, tt.wantRegion)
			}
			if account != tt.wantAccount {
				t.Errorf("account = %q, want %q", account, tt.wantAccount)
			}
			if chdir != tt.wantChdir {
				t.Errorf("chdir = %q, want %q", chdir, tt.wantChdir)
			}
		})
	}
}

func TestResolveRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	if got := resolveRegion("us-west-2"); got != "us-west-2" {
		t.Errorf("flag should win: got %q", got)
	}
	if got := resolveRegion(""); got != "us-east-1" {
		t.Errorf("default should be us-east-1: got %q", got)
	}
	t.Setenv("AWS_REGION", "eu-central-1")
	if got := resolveRegion(""); got != "eu-central-1" {
		t.Errorf("env fallback: got %q", got)
	}
	if got := resolveRegion("ap-south-1"); got != "ap-south-1" {
		t.Errorf("flag over env: got %q", got)
	}
}

func TestResolveAccount(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")

	got, err := resolveAccount("111111111111")
	if err != nil || got != "111111111111" {
		t.Errorf("valid flag: got %q err %v", got, err)
	}

	if _, err := resolveAccount("12345"); err == nil {
		t.Errorf("invalid account should error")
	}

	got, err = resolveAccount("")
	if err != nil || got != "test" {
		t.Errorf("default: got %q err %v", got, err)
	}

	t.Setenv("AWS_ACCESS_KEY_ID", "not-12-digits")
	got, err = resolveAccount("")
	if err != nil || got != "not-12-digits" {
		t.Errorf("mock-looking env value passes through unchanged: got %q err %v", got, err)
	}

	// A real-looking access key from the environment must be deactivated so it
	// never reaches LocalStack (AKIA… → LKIA…).
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	got, err = resolveAccount("")
	if err != nil || got != "LKIAIOSFODNN7EXAMPLE" {
		t.Errorf("real env access key should be deactivated: got %q err %v", got, err)
	}
}
