package endpoint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestS3Addressing(t *testing.T) {
	tests := []struct {
		endpoint      string
		wantPathStyle bool
		wantS3        string
	}{
		{"http://127.0.0.1:4566", true, "http://127.0.0.1:4566"},
		{"http://localhost:4566", true, "http://localhost:4566"},
		{"http://localhost.localstack.cloud:4566", false, "http://s3.localhost.localstack.cloud:4566"},
		{"https://localstack.cloud", false, "https://s3.localstack.cloud"},
		{"http://s3.localhost.localstack.cloud:4566", false, "http://s3.localhost.localstack.cloud:4566"},
		{"", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			pathStyle, s3 := S3Addressing(tt.endpoint)
			assert.Equal(t, tt.wantPathStyle, pathStyle)
			assert.Equal(t, tt.wantS3, s3)
		})
	}
}
