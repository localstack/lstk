package output

import (
	"io"
	"os"

	"golang.org/x/term"
)

func terminalWidth() int {
	return terminalWidthForWriter(os.Stdout)
}

func terminalWidthForWriter(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return 0
	}
	if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
		return w
	}
	return 0
}

func writerSupportsANSI(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// truncate clips the string to max runes with a trailing "…" for table cell fitting.
// Unlike hardWrap/softWrap (which break long text across multiple lines),
// truncate discards the overflow so each table column stays within its budget.
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
