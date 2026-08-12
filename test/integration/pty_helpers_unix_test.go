//go:build !windows

package integration_test

import (
	"os/exec"
	"syscall"
)

// makeSessionLeader makes cmd start as a session leader with its stdin (the
// PTY slave) as the controlling terminal, matching what creack's pty.Start
// always did (xpty's UnixPty.Start only wires the stdio). Without a
// controlling terminal the line discipline has no foreground process group to
// deliver Ctrl-C (0x03 typed into the master) to, so tests that stop a
// streaming child that way would hang instead.
func makeSessionLeader(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
}
