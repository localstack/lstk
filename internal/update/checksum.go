package update

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// parseChecksums parses a goreleaser-style checksums manifest: one
// "<sha256-hex>  <filename>" entry per line. It tolerates blank lines, CRLF
// line endings, and the "*" binary-mode marker some sha256sum implementations
// prepend to filenames. Returns a map of filename to lowercase hex digest.
func parseChecksums(r io.Reader) (map[string]string, error) {
	sums := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("malformed checksum manifest at line %d", lineNo)
		}
		sum := strings.ToLower(fields[0])
		if !isSHA256Hex(sum) {
			return nil, fmt.Errorf("malformed checksum manifest at line %d: invalid SHA-256 digest", lineNo)
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == "" {
			return nil, fmt.Errorf("malformed checksum manifest at line %d: empty filename", lineNo)
		}
		sums[name] = sum
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading checksum manifest: %w", err)
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("checksum manifest is empty")
	}
	return sums, nil
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
