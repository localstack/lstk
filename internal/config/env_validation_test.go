package config

import "testing"

func TestValidateNamedEnvs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		envs    map[string]map[string]string
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", map[string]map[string]string{}, false},
		{
			name: "valid",
			envs: map[string]map[string]string{
				"debug": {"ls_log": "trace", "debug": "1"},
				"ci":    {"services": "s3,sqs"},
			},
			wantErr: false,
		},
		{
			name:    "control char in value",
			envs:    map[string]map[string]string{"bad": {"debug": "1\x00"}},
			wantErr: true,
		},
		{
			name:    "hyphen in key",
			envs:    map[string]map[string]string{"bad": {"my-key": "1"}},
			wantErr: true,
		},
		{
			name:    "equals in key",
			envs:    map[string]map[string]string{"bad": {"a=b": "1"}},
			wantErr: true,
		},
		{
			name:    "key starts with digit",
			envs:    map[string]map[string]string{"bad": {"1var": "1"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateNamedEnvs(tt.envs)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateNamedEnvs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
