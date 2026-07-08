package container

import (
	"runtime"
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

func TestLogTailWritePeakAllocationBoundedByMaxBytes(t *testing.T) {
	// Write must not allocate proportionally to len(p) on an oversized single
	// write — the old "append-then-trim" implementation grows buf to
	// len(existing)+len(p) before trimming back down, spiking peak memory well
	// past maxBytes for one large write. Bytes allocated per call should stay
	// close to maxBytes regardless of how large p is.
	const maxBytes = 1024
	small := make([]byte, maxBytes/2)
	huge := make([]byte, 200*maxBytes)

	allocsFor := func(p []byte) float64 {
		lt := newLogTail(maxBytes)
		return testing.AllocsPerRun(5, func() {
			_, _ = lt.Write(p)
		})
	}

	bytesAllocFor := func(p []byte) uint64 {
		lt := newLogTail(maxBytes)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, _ = lt.Write(p)
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}

	t.Logf("allocs/run small=%v huge=%v", allocsFor(small), allocsFor(huge))

	smallBytes := bytesAllocFor(small)
	hugeBytes := bytesAllocFor(huge)
	t.Logf("bytes allocated small=%d huge=%d (len(huge)=%d)", smallBytes, hugeBytes, len(huge))

	// A single write must never need to allocate anywhere near len(p) bytes;
	// it should stay within a small constant multiple of maxBytes.
	assert.Lessf(t, hugeBytes, uint64(10*maxBytes),
		"Write allocated %d bytes for a %d-byte input with maxBytes=%d — peak memory scales with input size instead of being bounded",
		hugeBytes, len(huge), maxBytes)
}

func TestLogTailHandlesRepeatedOversizedWrites(t *testing.T) {
	lt := newLogTail(5)

	n, err := lt.Write([]byte("abcdefgh"))
	assert.NoError(t, err)
	assert.Equal(t, 8, n)
	assert.Equal(t, "defgh", lt.String())

	n, err = lt.Write([]byte("123456789"))
	assert.NoError(t, err)
	assert.Equal(t, 9, n)
	assert.Equal(t, "56789", lt.String())
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
