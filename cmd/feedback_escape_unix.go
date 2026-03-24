//go:build darwin || linux

package cmd

import (
	"os"

	"golang.org/x/sys/unix"
)

func readEscapeSequence(in *os.File) (bool, error) {
	fd := int(in.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		return false, err
	}
	defer func() { _ = unix.SetNonblock(fd, false) }()

	buf := make([]byte, 8)
	n, err := unix.Read(fd, buf)
	if err == unix.EAGAIN || err == unix.EWOULDBLOCK || n == 0 {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}
