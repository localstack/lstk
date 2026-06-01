package cmd

import (
	"reflect"
	"testing"
)

func TestStripNonInteractiveFlag(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantArgs        []string
		wantNonInteract bool
	}{
		{
			name:            "absent",
			args:            []string{"s3", "ls"},
			wantArgs:        []string{"s3", "ls"},
			wantNonInteract: false,
		},
		{
			name:            "bare flag is stripped and enables non-interactive",
			args:            []string{"--non-interactive", "s3", "ls"},
			wantArgs:        []string{"s3", "ls"},
			wantNonInteract: true,
		},
		{
			name:            "flag among aws args is stripped",
			args:            []string{"s3", "ls", "--non-interactive", "--recursive"},
			wantArgs:        []string{"s3", "ls", "--recursive"},
			wantNonInteract: true,
		},
		{
			name:            "does not strip a similarly named aws flag",
			args:            []string{"s3", "ls", "--non-interactive-mode"},
			wantArgs:        []string{"s3", "ls", "--non-interactive-mode"},
			wantNonInteract: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotNonInteract := stripNonInteractiveFlag(tt.args)
			if gotNonInteract != tt.wantNonInteract {
				t.Errorf("nonInteractive = %v, want %v", gotNonInteract, tt.wantNonInteract)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("args = %v, want %v", gotArgs, tt.wantArgs)
			}
		})
	}
}
