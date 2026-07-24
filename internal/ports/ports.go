package ports

import (
	"fmt"
	"net"
	"runtime"
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

// InspectCommand returns a shell command the user can run to identify the
// process currently listening on the given port, tailored to the host OS.
// On Windows it uses netstat; elsewhere (macOS, Linux, *BSD) it uses lsof.
func InspectCommand(port string) string {
	return inspectCommand(runtime.GOOS, port)
}

func inspectCommand(goos, port string) string {
	if goos == "windows" {
		return fmt.Sprintf("netstat -ano | findstr :%s", port)
	}
	// Privileged ports (<1024, e.g. 443) are typically held by root-owned
	// processes such as sshd; lsof only reports other users' sockets under
	// sudo, so suggest sudo there to avoid an empty result.
	if p, err := strconv.Atoi(port); err == nil && p < 1024 {
		return fmt.Sprintf("sudo lsof -i tcp:%s", port)
	}
	return fmt.Sprintf("lsof -i tcp:%s", port)
}
