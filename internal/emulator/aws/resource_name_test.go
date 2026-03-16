package aws

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractResourceName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"my-bucket", "my-bucket"},
		{"arn:aws:sns:us-east-1:000000000000:my-topic", "my-topic"},
		{"arn:aws:iam::000000000000:role/my-role", "my-role"},
		{"arn:aws:lambda:us-east-1:000000000000:function:my-func", "my-func"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, extractResourceName(tt.input), "extractResourceName(%q)", tt.input)
	}
}
