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

// setCttyToStdout points Setctty's controlling-terminal ioctl at the child's
// fd 1 instead of the default fd 0. Needed by PTY tests that redirect stdin
// off the PTY (e.g. to /dev/null): fd 0 is then not a terminal and exec would
// fail, while stdout still holds the PTY slave. Call before startCmdInPTY —
// makeSessionLeader preserves an existing SysProcAttr.
func setCttyToStdout(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Ctty = 1
}
