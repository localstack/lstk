package container

import (
	"testing"
)

// FuzzParseLogLine targets string slicing in parseLogLine.
// Known risk: sepIdx+6 skip, bracketEnd+1, colonIdx boundary — all sequential
// string offsets that could panic on crafted input.
func FuzzParseLogLine(f *testing.F) {
	f.Add("")
	f.Add(" --- [")
	f.Add(" --- []")
	f.Add(" --- [ ]")
	f.Add(" --- [x] : ")
	f.Add(" --- [x]")
	f.Add(" --- [x] no colon")
	f.Add(" --- [x] : logger")
	f.Add("INFO --- [")
	f.Add("INFO --- []")
	f.Add("INFO --- [] : ")
	f.Add("2026-03-16T17:56:00.810  INFO --- [  MainThread] l.p.c.extensions.plugins   : loaded 0 extensions")
	f.Add("Docker not available")
	f.Add(string(make([]byte, 0)))
	f.Add(string([]byte{0, 0, 0}))
	// edge: " --- [" at very end
	f.Add("x --- [")
	// edge: bracket immediately after separator
	f.Add("x --- []")
	// edge: colon right after bracket
	f.Add("x --- [y] : ")
	// edge: only whitespace
	f.Add("   ")
	// edge: repeated separator patterns
	f.Add(" --- [ --- [ --- [")
	// edge: very long line
	f.Add(string(make([]byte, 10000)))

	f.Fuzz(func(t *testing.T, line string) {
		// Must never panic
		level, logger := parseLogLine(line)
		_ = level
		_ = logger

		// Also exercise shouldFilter
		_ = shouldFilter(line)
	})
}
