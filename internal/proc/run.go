// Package proc runs wrapped external tools (aws, terraform, cdk, sam, az, and
// extensions) so that termination signals reach the child gracefully instead of
// hard-killing it.
package proc

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/localstack/lstk/internal/terminal"
)

// forwardedSignals are relayed to the wrapped child so it can shut down
// gracefully (e.g. terraform releasing its state lock) rather than being killed
// abruptly.
var forwardedSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// Run starts cmd, waits for it to exit, and returns the same error cmd.Run
// would. It ensures SIGINT/SIGTERM reach the wrapped tool so it can clean up
// instead of being SIGKILLed the instant lstk's context is cancelled.
//
// When cmd was created with exec.CommandContext, its default Cancel hard-kills
// the child (SIGKILL) as soon as the context is done — which is exactly what
// happens on Ctrl-C, since lstk cancels its root context on SIGINT/SIGTERM. Run
// disarms that: a cancelled context no longer kills the child, so the tool
// terminates from the signal it receives and lstk waits for it to finish its own
// shutdown. Returning os.ErrProcessDone from Cancel both suppresses the kill and
// avoids injecting context.Canceled into the wait result, so the tool's real
// exit code is preserved.
//
// In an interactive terminal the child already receives the signal via the
// controlling terminal's foreground process group, so Run does not forward
// there: a second, near-simultaneous SIGINT makes tools like terraform abort
// immediately instead of cleaning up. Forwarding therefore covers only the
// non-interactive case (e.g. a supervisor sending kill to lstk alone), mirroring
// the model documented in npm/launcher.js.
func Run(cmd *exec.Cmd) error {
	// Disarm exec.CommandContext's SIGKILL-on-cancel (no-op for a plain
	// exec.Command, whose Cancel is nil).
	if cmd.Cancel != nil {
		cmd.Cancel = func() error { return os.ErrProcessDone }
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	if !terminal.IsTerminal(os.Stdin) {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, forwardedSignals...)
		defer signal.Stop(sigCh)

		done := make(chan struct{})
		defer close(done)
		go func() {
			for {
				select {
				case sig := <-sigCh:
					_ = cmd.Process.Signal(sig)
				case <-done:
					return
				}
			}
		}()
	}

	return cmd.Wait()
}
