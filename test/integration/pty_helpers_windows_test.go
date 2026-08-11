//go:build windows

package integration_test

import "os/exec"

// makeSessionLeader is a no-op on Windows: ConPTY attaches the spawned
// process to the pseudo console itself, and console control events (Ctrl-C)
// reach every attached process without POSIX session/controlling-terminal
// setup.
func makeSessionLeader(*exec.Cmd) {}
