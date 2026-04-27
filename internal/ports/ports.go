package ports

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// CheckAvailable reports whether all given port specs are free to bind.
// Each spec is a single port ("443") or an inclusive range ("4510-4559").
// Returns the first port found to be in use and a non-nil error; returns an
// empty string and nil if all ports are available.
func CheckAvailable(specs ...string) (string, error) {
	for _, spec := range specs {
		if lo, hi, ok := parseRange(spec); ok {
			for p := lo; p <= hi; p++ {
				port := strconv.Itoa(p)
				if err := dial(port); err != nil {
					return port, err
				}
			}
		} else {
			if err := dial(spec); err != nil {
				return spec, err
			}
		}
	}
	return "", nil
}

func parseRange(spec string) (lo, hi int, ok bool) {
	loStr, hiStr, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false
	}
	lo, err1 := strconv.Atoi(loStr)
	hi, err2 := strconv.Atoi(hiStr)
	if err1 != nil || err2 != nil || lo > hi {
		return 0, 0, false
	}
	return lo, hi, true
}

func dial(port string) error {
	conn, err := net.DialTimeout("tcp", "localhost:"+port, time.Second)
	if err != nil {
		return nil
	}
	_ = conn.Close()
	return fmt.Errorf("port %s already in use", port)
}
