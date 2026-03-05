package wrap

import (
	"strings"
	"unicode/utf8"
)

// HardWrap inserts newlines so that no output line exceeds maxWidth runes.
// It breaks at exact rune boundaries with no regard for word boundaries.
func HardWrap(s string, maxWidth int) string {
	rs := []rune(s)
	if maxWidth <= 0 || len(rs) <= maxWidth {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(rs); i += maxWidth {
		if i > 0 {
			sb.WriteByte('\n')
		}
		end := i + maxWidth
		if end > len(rs) {
			end = len(rs)
		}
		sb.WriteString(string(rs[i:end]))
	}
	return sb.String()
}

// SoftWrap breaks text into lines at word boundaries, falling back to hard
// breaks for words longer than maxWidth.
func SoftWrap(text string, maxWidth int) []string {
	if maxWidth <= 0 || utf8.RuneCountInString(text) <= maxWidth {
		return []string{text}
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	var lines []string
	var current strings.Builder
	currentRunes := 0

	for _, word := range words {
		runes := []rune(word)
		wordRunes := len(runes)
		if wordRunes > maxWidth {
			if currentRunes > 0 {
				lines = append(lines, current.String())
				current.Reset()
				currentRunes = 0
			}
			for len(runes) > 0 {
				chunk := runes
				if len(chunk) > maxWidth {
					chunk = runes[:maxWidth]
				}
				runes = runes[len(chunk):]
				if len(runes) > 0 || len(chunk) == maxWidth {
					lines = append(lines, string(chunk))
				} else {
					current.WriteString(string(chunk))
					currentRunes = len(chunk)
				}
			}
			continue
		}
		if currentRunes == 0 {
			current.WriteString(word)
			currentRunes = wordRunes
			continue
		}
		if currentRunes+1+wordRunes > maxWidth {
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(word)
			currentRunes = wordRunes
		} else {
			current.WriteByte(' ')
			current.WriteString(word)
			currentRunes += 1 + wordRunes
		}
	}
	if currentRunes > 0 {
		lines = append(lines, current.String())
	}

	return lines
}
