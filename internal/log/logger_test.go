package log

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInfo_Writes(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.Info("starting up")
	assert.Contains(t, buf.String(), "[INFO] starting up\n")
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \[INFO\]`, buf.String())
}

func TestError_Writes(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.Error("license failed: %s", "forbidden")
	assert.Contains(t, buf.String(), "[ERROR] license failed: forbidden\n")
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \[ERROR\]`, buf.String())
}

func TestNop_DoesNotPanic(t *testing.T) {
	l := Nop()
	l.Info("test %s", "arg")
	l.Error("test %s", "arg")
}
