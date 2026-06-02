//go:build !windows

package telemetry

import "syscall"

// Setsid puts the child in a new session so it isn't killed when the parent's
// controlling terminal goes away.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
