//go:build windows

package telemetry

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP: detach from the parent's console
// and put the child in its own process group so Ctrl+C in the parent doesn't
// signal it.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
}
