//go:build windows

package auth

import "os"

func lockFile(f *os.File) error {
	// Windows LockFileEx requires unsafe/syscall wiring; for now token
	// file contention on Windows is unlikely enough to skip locking.
	return nil
}

func unlockFile(f *os.File) error {
	return nil
}
