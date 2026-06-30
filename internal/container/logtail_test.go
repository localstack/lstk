package container

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogTailRetainsContentUnderLimit(t *testing.T) {
	lt := newLogTail(1024)
	n, err := lt.Write([]byte("hello "))
	assert.NoError(t, err)
	assert.Equal(t, 6, n)
	_, _ = lt.Write([]byte("world"))
	assert.Equal(t, "hello world", lt.String())
}

func TestLogTailKeepsOnlyMostRecentBytes(t *testing.T) {
	lt := newLogTail(5)
	_, _ = lt.Write([]byte("abc"))
	_, _ = lt.Write([]byte("defgh"))
	// Only the last 5 bytes are retained.
	assert.Equal(t, "defgh", lt.String())

	_, _ = lt.Write([]byte("XY"))
	assert.Equal(t, "fghXY", lt.String())
}

func TestLogTailWriteReportsFullLength(t *testing.T) {
	// Write must report len(p) even when the tail is trimmed, so an io.Writer
	// consumer (stdcopy) does not treat it as a short write.
	lt := newLogTail(4)
	n, err := lt.Write([]byte("abcdefgh"))
	assert.NoError(t, err)
	assert.Equal(t, 8, n)
	assert.Equal(t, "efgh", lt.String())
}

func TestLogTailConcurrentWritesAreSafe(t *testing.T) {
	lt := newLogTail(64)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = lt.Write([]byte(strings.Repeat("x", 8)))
			_ = lt.String()
		}()
	}
	wg.Wait()
	assert.LessOrEqual(t, len(lt.String()), 64)
}
