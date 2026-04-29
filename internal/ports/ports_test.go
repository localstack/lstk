package ports

import (
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRange(t *testing.T) {
	tests := []struct {
		spec   string
		wantLo int
		wantHi int
		wantOk bool
	}{
		{"4510-4559", 4510, 4559, true},
		{"443-443", 443, 443, true},
		{"4566", 0, 0, false},
		{"4566/tcp", 0, 0, false},
		{"4559-4510", 0, 0, false}, // reversed range
		{"abc-def", 0, 0, false},
		{"", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			lo, hi, ok := parseRange(tt.spec)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantLo, lo)
				assert.Equal(t, tt.wantHi, hi)
			}
		})
	}
}

func TestCheckAvailable(t *testing.T) {
	busy1 := bindPort(t)
	busy2 := bindPort(t)
	free1 := freePort(t)

	freeRange := free1 + "-" + free1
	busyRange := busy1 + "-" + busy1

	tests := []struct {
		name         string
		specs        []string
		wantErr      bool
		wantConflict string
	}{
		{"free port", []string{free1}, false, ""},
		{"busy port", []string{busy1}, true, busy1},
		{"free range", []string{freeRange}, false, ""},
		{"busy range", []string{busyRange}, true, busy1},
		{"multiple specs: stops at first conflict", []string{busy1, busy2}, true, busy1},
		{"free then busy", []string{free1, busy1}, true, busy1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conflict, err := CheckAvailable(tt.specs...)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.wantConflict, conflict)
			} else {
				assert.NoError(t, err)
				assert.Empty(t, conflict)
			}
		})
	}
}

// bindPort opens a listener on a random port and keeps it open for the test duration.
func bindPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// freePort allocates and immediately releases a port, returning it as a string.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	require.NoError(t, ln.Close())
	return port
}

