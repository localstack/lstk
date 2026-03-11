package output

import (
	"os"

	"golang.org/x/term"
)

func terminalWidth() int {
	for _, fd := range []uintptr{os.Stdout.Fd(), os.Stderr.Fd()} {
		if w, _, err := term.GetSize(int(fd)); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

func displayWidth(s string) int {
	return len([]rune(s))
}
