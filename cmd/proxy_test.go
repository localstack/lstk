package cmd

import (
	"reflect"
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
