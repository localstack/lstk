package cmd

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestStripGlobalFlags(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantArgs        []string
		wantNonInteract bool
		wantConfigPath  string
	}{
		{
			name:     "no global flags",
			args:     []string{"s3", "ls"},
			wantArgs: []string{"s3", "ls"},
		},
		{
			name:            "bare non-interactive is stripped",
			args:            []string{"--non-interactive", "s3", "ls"},
			wantArgs:        []string{"s3", "ls"},
			wantNonInteract: true,
		},
		{
			name:            "non-interactive among aws args is stripped",
			args:            []string{"s3", "ls", "--non-interactive", "--recursive"},
			wantArgs:        []string{"s3", "ls", "--recursive"},
			wantNonInteract: true,
		},
		{
			name:            "non-interactive with explicit true value",
			args:            []string{"--non-interactive=true", "s3", "ls"},
			wantArgs:        []string{"s3", "ls"},
			wantNonInteract: true,
		},
		{
			name:            "non-interactive with explicit false value",
			args:            []string{"--non-interactive=false", "s3", "ls"},
			wantArgs:        []string{"s3", "ls"},
			wantNonInteract: false,
		},
		{
			// --json is deliberately NOT an lstk global for proxy commands: it must
			// reach the wrapped tool untouched (e.g. Terraform's own -json/--json).
			name:     "bare json is left untouched",
			args:     []string{"--json", "s3", "ls"},
			wantArgs: []string{"--json", "s3", "ls"},
		},
		{
			name:     "json among aws args is left untouched",
			args:     []string{"s3", "ls", "--json", "--recursive"},
			wantArgs: []string{"s3", "ls", "--json", "--recursive"},
		},
		{
			name:     "json=value form is left untouched",
			args:     []string{"--json=true", "s3", "ls"},
			wantArgs: []string{"--json=true", "s3", "ls"},
		},
		{
			name:           "config with separate value",
			args:           []string{"--config", "/tmp/c.toml", "s3", "ls"},
			wantArgs:       []string{"s3", "ls"},
			wantConfigPath: "/tmp/c.toml",
		},
		{
			name:           "config with equals value",
			args:           []string{"--config=/tmp/c.toml", "s3", "ls"},
			wantArgs:       []string{"s3", "ls"},
			wantConfigPath: "/tmp/c.toml",
		},
		{
			name:           "config among aws args",
			args:           []string{"s3", "ls", "--config", "/tmp/c.toml"},
			wantArgs:       []string{"s3", "ls"},
			wantConfigPath: "/tmp/c.toml",
		},
		{
			name:            "both flags together",
			args:            []string{"--non-interactive", "--config=/tmp/c.toml", "s3", "ls"},
			wantArgs:        []string{"s3", "ls"},
			wantNonInteract: true,
			wantConfigPath:  "/tmp/c.toml",
		},
		{
			name:     "trailing config without value is dropped",
			args:     []string{"s3", "ls", "--config"},
			wantArgs: []string{"s3", "ls"},
		},
		{
			name:     "similarly named flags are left untouched",
			args:     []string{"s3", "ls", "--non-interactive-mode", "--config-file", "x"},
			wantArgs: []string{"s3", "ls", "--non-interactive-mode", "--config-file", "x"},
		},
		{
			// --config has no -c shorthand: -c must pass through so wrapped tools
			// that claim it keep working (CDK's -c/--context, SAM's -c/--cached).
			name:     "-c passes through to the wrapped tool",
			args:     []string{"synth", "-c", "env=prod"},
			wantArgs: []string{"synth", "-c", "env=prod"},
		},
		{
			name:     "-c=value passes through to the wrapped tool",
			args:     []string{"synth", "-c=env=prod"},
			wantArgs: []string{"synth", "-c=env=prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gf := stripGlobalFlags(tt.args)
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("args = %v, want %v", gotArgs, tt.wantArgs)
			}
			if gf.nonInteractive != tt.wantNonInteract {
				t.Errorf("nonInteractive = %v, want %v", gf.nonInteractive, tt.wantNonInteract)
			}
			if gf.configPath != tt.wantConfigPath {
				t.Errorf("configPath = %q, want %q", gf.configPath, tt.wantConfigPath)
			}
		})
	}
}

func TestStripLeadingProxyFlags(t *testing.T) {
	tests := []struct {
		name        string
		opts        leadingFlags
		args        []string
		wantRemain  []string
		wantRegion  string
		wantAccount string
		wantChdir   string
		wantErr     bool
	}{
		{
			name:       "no flags",
			opts:       leadingFlags{account: true, region: true, chdir: true},
			args:       []string{"plan"},
			wantRemain: []string{"plan"},
		},
		{
			name:       "region space form",
			opts:       leadingFlags{account: true, region: true, chdir: true},
			args:       []string{"--region", "us-west-2", "plan"},
			wantRemain: []string{"plan"},
			wantRegion: "us-west-2",
		},
		{
			name:       "region equals form",
			opts:       leadingFlags{account: true, region: true, chdir: true},
			args:       []string{"--region=us-west-2", "plan"},
			wantRemain: []string{"plan"},
			wantRegion: "us-west-2",
		},
		{
			name:        "both flags both forms",
			opts:        leadingFlags{account: true, region: true, chdir: true},
			args:        []string{"--region", "eu-west-1", "--account=111111111111", "apply", "-auto-approve"},
			wantRemain:  []string{"apply", "-auto-approve"},
			wantRegion:  "eu-west-1",
			wantAccount: "111111111111",
		},
		{
			name:       "flags after action are forwarded verbatim",
			opts:       leadingFlags{account: true, region: true, chdir: true},
			args:       []string{"plan", "--region", "us-west-2"},
			wantRemain: []string{"plan", "--region", "us-west-2"},
		},
		{
			name:    "region missing value",
			opts:    leadingFlags{account: true, region: true, chdir: true},
			args:    []string{"--region"},
			wantErr: true,
		},
		{
			name:    "account missing value",
			opts:    leadingFlags{account: true, region: true, chdir: true},
			args:    []string{"--region", "us-east-1", "--account"},
			wantErr: true,
		},
		{
			name:       "stops at first non-flag token",
			opts:       leadingFlags{account: true, region: true, chdir: true},
			args:       []string{"--account=111111111111", "apply", "--region", "x"},
			wantRemain: []string{"apply", "--region", "x"},
			// region stays empty because --region appears after the action
			wantAccount: "111111111111",
		},
		{
			name:       "chdir is read and kept in forwarded args",
			opts:       leadingFlags{account: true, region: true, chdir: true},
			args:       []string{"-chdir=infra", "plan"},
			wantRemain: []string{"-chdir=infra", "plan"},
			wantChdir:  "infra",
		},
		{
			name:        "chdir before leading flags keeps chdir and consumes flags",
			opts:        leadingFlags{account: true, region: true, chdir: true},
			args:        []string{"-chdir=infra", "--region", "us-west-2", "--account=111111111111", "apply"},
			wantRemain:  []string{"-chdir=infra", "apply"},
			wantRegion:  "us-west-2",
			wantAccount: "111111111111",
			wantChdir:   "infra",
		},
		{
			name:       "chdir after leading flags keeps chdir and consumes flags",
			opts:       leadingFlags{account: true, region: true, chdir: true},
			args:       []string{"--region", "us-west-2", "-chdir=infra", "plan"},
			wantRemain: []string{"-chdir=infra", "plan"},
			wantRegion: "us-west-2",
			wantChdir:  "infra",
		},
		{
			name:       "space-separated chdir form is not interpreted and is forwarded",
			opts:       leadingFlags{account: true, region: true, chdir: true},
			args:       []string{"-chdir", "infra", "plan"},
			wantRemain: []string{"-chdir", "infra", "plan"},
			// scanning stops at the unrecognized -chdir token; nothing consumed.
		},
		{
			name:       "cdk and sam do not recognize chdir",
			opts:       leadingFlags{account: true, region: true},
			args:       []string{"-chdir=infra", "deploy"},
			wantRemain: []string{"-chdir=infra", "deploy"},
		},
		// `lstk aws` opts out of --region: the AWS CLI has its own global
		// --region and must receive it untouched in every position.
		{
			name:       "aws leaves a leading region flag in place",
			opts:       leadingFlags{account: true},
			args:       []string{"--region", "us-west-2", "s3", "ls"},
			wantRemain: []string{"--region", "us-west-2", "s3", "ls"},
		},
		{
			name:       "aws leaves a leading region equals form in place",
			opts:       leadingFlags{account: true},
			args:       []string{"--region=us-west-2", "s3", "ls"},
			wantRemain: []string{"--region=us-west-2", "s3", "ls"},
		},
		{
			name:        "aws consumes a leading account flag",
			opts:        leadingFlags{account: true},
			args:        []string{"--account", "111111111111", "s3", "ls"},
			wantRemain:  []string{"s3", "ls"},
			wantAccount: "111111111111",
		},
		{
			name:        "aws consumes a leading account equals form",
			opts:        leadingFlags{account: true},
			args:        []string{"--account=111111111111", "s3", "ls"},
			wantRemain:  []string{"s3", "ls"},
			wantAccount: "111111111111",
		},
		{
			// A --account after the service belongs to the AWS CLI
			// (e.g. `organizations describe-account --account-id`).
			name:       "aws forwards a non-leading account flag",
			opts:       leadingFlags{account: true},
			args:       []string{"organizations", "describe-account", "--account-id", "111111111111"},
			wantRemain: []string{"organizations", "describe-account", "--account-id", "111111111111"},
		},
		{
			name:    "aws account missing value",
			opts:    leadingFlags{account: true},
			args:    []string{"--account"},
			wantErr: true,
		},
		{
			// No caller disables it today, but the field gates the branch, so
			// pin what clearing it means rather than leaving it untested.
			name:       "account not recognized when disabled",
			opts:       leadingFlags{},
			args:       []string{"--account", "111111111111", "s3", "ls"},
			wantRemain: []string{"--account", "111111111111", "s3", "ls"},
		},
		// The leading run ends at the action, not at the first token lstk does
		// not own, so lstk's flags work in any order relative to the wrapped
		// tool's own. Before this, --region first made --account leak to the
		// AWS CLI, which rejects it as an unknown option.
		{
			name:        "aws account after the tool's own region flag",
			opts:        leadingFlags{account: true},
			args:        []string{"--region", "eu-west-1", "--account", "555555555555", "sqs", "create-queue", "--queue-name", "bah"},
			wantRemain:  []string{"--region", "eu-west-1", "sqs", "create-queue", "--queue-name", "bah"},
			wantAccount: "555555555555",
		},
		{
			name:        "aws account before the tool's own region flag",
			opts:        leadingFlags{account: true},
			args:        []string{"--account", "555555555555", "--region", "eu-west-1", "sqs", "create-queue"},
			wantRemain:  []string{"--region", "eu-west-1", "sqs", "create-queue"},
			wantAccount: "555555555555",
		},
		{
			name:        "aws account after an equals-form tool flag",
			opts:        leadingFlags{account: true},
			args:        []string{"--region=eu-west-1", "--account=555555555555", "sqs", "ls"},
			wantRemain:  []string{"--region=eu-west-1", "sqs", "ls"},
			wantAccount: "555555555555",
		},
		{
			name:        "aws account after a valueless tool flag",
			opts:        leadingFlags{account: true},
			args:        []string{"--debug", "--account", "555555555555", "s3", "ls"},
			wantRemain:  []string{"--debug", "s3", "ls"},
			wantAccount: "555555555555",
		},
		{
			name:        "aws account after several tool flags",
			opts:        leadingFlags{account: true},
			args:        []string{"--output", "json", "--profile", "p", "--account", "555555555555", "s3", "ls"},
			wantRemain:  []string{"--output", "json", "--profile", "p", "s3", "ls"},
			wantAccount: "555555555555",
		},
		// The AWS CLI defines a real --account on ten operations. Scanning
		// absorbs at most one bare token per flag, so it always halts at the
		// second consecutive bare token — the operation — and can never reach
		// a parameter that follows it.
		{
			name:       "aws never claims a genuine account parameter",
			opts:       leadingFlags{account: true},
			args:       []string{"events", "create-partner-event-source", "--name", "x", "--account", "123456789012"},
			wantRemain: []string{"events", "create-partner-event-source", "--name", "x", "--account", "123456789012"},
		},
		{
			name:       "aws never claims a genuine account parameter behind a tool flag",
			opts:       leadingFlags{account: true},
			args:       []string{"--region", "eu-west-1", "events", "create-partner-event-source", "--account", "123456789012"},
			wantRemain: []string{"--region", "eu-west-1", "events", "create-partner-event-source", "--account", "123456789012"},
		},
		{
			name:       "aws never claims a genuine account parameter behind a valueless tool flag",
			opts:       leadingFlags{account: true},
			args:       []string{"--debug", "redshift", "authorize-endpoint-access", "--account", "123456789012"},
			wantRemain: []string{"--debug", "redshift", "authorize-endpoint-access", "--account", "123456789012"},
		},
		// The same fix reaches the other three proxies, whose own global flags
		// walled off lstk's flags in exactly the same way.
		{
			name:        "sam account after the tool's own global flag",
			opts:        leadingFlags{account: true, region: true},
			args:        []string{"--debug", "--account", "555555555555", "build"},
			wantRemain:  []string{"--debug", "build"},
			wantAccount: "555555555555",
		},
		{
			name:        "terraform lstk flags after chdir",
			opts:        leadingFlags{account: true, region: true, chdir: true},
			args:        []string{"-chdir=infra", "--region", "us-west-2", "--account", "111111111111", "apply"},
			wantRemain:  []string{"-chdir=infra", "apply"},
			wantRegion:  "us-west-2",
			wantAccount: "111111111111",
			wantChdir:   "infra",
		},
		{
			// -chdir carries its value inline, so the action after it must not
			// be mistaken for that value and swallowed.
			name:       "terraform action after chdir is not taken as its value",
			opts:       leadingFlags{account: true, region: true, chdir: true},
			args:       []string{"-chdir=infra", "plan", "--region", "us-west-2"},
			wantRemain: []string{"-chdir=infra", "plan", "--region", "us-west-2"},
			wantChdir:  "infra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remain, region, account, chdir, err := stripLeadingProxyFlags(tt.args, tt.opts)
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

func TestRejectPreSubcommandFlags(t *testing.T) {
	tests := []struct {
		name     string
		osArgs   []string
		calledAs string
		flags    []string
		wantErr  bool
		wantMsg  string
	}{
		{
			name:     "terraform rejects a pre-subcommand region",
			osArgs:   []string{"lstk", "--region", "us-west-2", "terraform", "plan"},
			calledAs: "terraform",
			flags:    []string{"--region", "--account"},
			wantErr:  true,
			wantMsg:  "--region and --account must appear after the terraform subcommand (e.g. `lstk terraform --region us-west-2 ...`)",
		},
		{
			name:     "aws names only the flag it claims",
			osArgs:   []string{"lstk", "--account", "111111111111", "aws", "s3", "ls"},
			calledAs: "aws",
			flags:    []string{"--account"},
			wantErr:  true,
			wantMsg:  "--account must appear after the aws subcommand (e.g. `lstk aws --account 111111111111 ...`)",
		},
		{
			name:     "equals form is rejected too",
			osArgs:   []string{"lstk", "--account=111111111111", "aws", "s3", "ls"},
			calledAs: "aws",
			flags:    []string{"--account"},
			wantErr:  true,
		},
		{
			// `lstk aws` does not claim --region, so a pre-command one is not its
			// business; Cobra's own root flag parsing rejects the unknown flag.
			name:     "aws does not reject a pre-subcommand region",
			osArgs:   []string{"lstk", "--region", "us-west-2", "aws", "s3", "ls"},
			calledAs: "aws",
			flags:    []string{"--account"},
		},
		{
			name:     "a flag after the subcommand is fine",
			osArgs:   []string{"lstk", "aws", "--account", "111111111111", "s3", "ls"},
			calledAs: "aws",
			flags:    []string{"--account"},
		},
		{
			name:     "no subcommand token found",
			osArgs:   []string{"lstk", "--account", "111111111111"},
			calledAs: "aws",
			flags:    []string{"--account"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := os.Args
			t.Cleanup(func() { os.Args = orig })
			os.Args = tt.osArgs

			err := rejectPreSubcommandFlags(tt.calledAs, tt.flags...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantMsg != "" && err.Error() != tt.wantMsg {
				t.Errorf("message =\n  %q\nwant\n  %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestResolveAccountSelection(t *testing.T) {
	tests := []struct {
		name         string
		flag         string
		env          string
		wantAccount  string
		wantSelected bool
		wantErr      bool
	}{
		{
			name:         "valid flag selects",
			flag:         "111111111111",
			wantAccount:  "111111111111",
			wantSelected: true,
		},
		{
			name:    "invalid flag errors",
			flag:    "12345",
			wantErr: true,
		},
		{
			name:        "no flag and no env falls back to test",
			wantAccount: "test",
		},
		{
			name:         "twelve-digit env value selects",
			env:          "111111111111",
			wantAccount:  "111111111111",
			wantSelected: true,
		},
		{
			name:         "flag beats env",
			flag:         "222222222222",
			env:          "111111111111",
			wantAccount:  "222222222222",
			wantSelected: true,
		},
		{
			// A mock-looking value passes through but is not an account
			// selection, so it must not displace a configured profile.
			name:        "non-account env value passes through without selecting",
			env:         "not-12-digits",
			wantAccount: "not-12-digits",
		},
		{
			// A real key from the environment is deactivated so it never
			// reaches LocalStack (AKIA… → LKIA…), and never selects.
			name:        "real env access key is deactivated and does not select",
			env:         "AKIAIOSFODNN7EXAMPLE",
			wantAccount: "LKIAIOSFODNN7EXAMPLE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AWS_ACCESS_KEY_ID", tt.env)

			account, selected, err := resolveAccountSelection(tt.flag)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), "12-digit AWS account id") {
					t.Errorf("error should name the 12-digit rule: %v", err)
				}
				return
			}
			if account != tt.wantAccount {
				t.Errorf("account = %q, want %q", account, tt.wantAccount)
			}
			if selected != tt.wantSelected {
				t.Errorf("selected = %v, want %v", selected, tt.wantSelected)
			}
		})
	}
}
