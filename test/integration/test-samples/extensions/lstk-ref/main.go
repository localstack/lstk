// Command lstk-ref is a reference lstk extension used by lstk's own integration
// tests. It exercises the manifest-free contract: lstk resolves it as the `ref`
// extension (`lstk ref ...`), forwards arguments verbatim, and conveys runtime
// context via the LSTK_EXT_API_VERSION + LSTK_EXT_CONTEXT (JSON) environment
// variables. The extension decodes that context, echoes it back so tests can
// assert on it, and shows the recommended self-authorization pattern. The prose
// author guide in docs/extensions-authoring.md is the canonical reference for
// extension authors; this binary exists for tests.
//
// Subcommands:
//
//	(default)   Echo the received args and decoded context, then exit 0.
//	exit N      Echo, then exit with status N (for exit-code propagation tests).
//	auth        Perform a stubbed self-authorization: succeed (exit 0) only when
//	            the conveyed context carries an auth token, otherwise refuse
//	            (exit 13). A real extension would verify the token server-side
//	            against the LocalStack platform — authorization must never rely on
//	            lstk, which is open source and rebuildable.
//	signal-wait Print a readiness marker, then exit 40 + the number of
//	            SIGINT/SIGTERM received (40 = none after a deadline, 41 = exactly
//	            one, 42 = a double signal). Backs lstk's signal-forwarding tests.
//
// The extension also self-enforces contract compatibility: it requires
// LSTK_EXT_API_VERSION >= minAPIVersion and refuses to run otherwise, rather
// than relying on lstk to gate it.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// minAPIVersion is the lowest contract version this extension supports. lstk
// advertises its version via LSTK_EXT_API_VERSION; the extension checks it itself.
const minAPIVersion = 1

// exitNotAuthorized is the status the stubbed self-authorization returns when no
// auth token was conveyed.
const exitNotAuthorized = 13

// emulator mirrors one entry of the LSTK_EXT_CONTEXT `emulators` array.
type emulator struct {
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
	Port     string `json:"port"`
}

// extContext mirrors the LSTK_EXT_CONTEXT JSON object lstk conveys.
type extContext struct {
	ConfigDir      string     `json:"configDir"`
	AuthToken      string     `json:"authToken"`
	NonInteractive bool       `json:"nonInteractive"`
	JSON           bool       `json:"json"`
	Emulators      []emulator `json:"emulators"`
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if v, err := strconv.Atoi(os.Getenv("LSTK_EXT_API_VERSION")); err == nil && v < minAPIVersion {
		fmt.Fprintf(os.Stderr, "lstk-ref: requires LSTK_EXT_API_VERSION >= %d, got %d\n", minAPIVersion, v)
		return 1
	}

	ctx := decodeContext()
	echo(args, ctx)

	if len(args) == 0 {
		return 0
	}
	switch args[0] {
	case "exit":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "lstk-ref: exit requires a status code")
			return 1
		}
		code, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "lstk-ref: invalid exit code %q\n", args[1])
			return 1
		}
		return code
	case "auth":
		if ctx.AuthToken == "" {
			fmt.Fprintln(os.Stderr, "lstk-ref: not authorized (no auth token conveyed)")
			return exitNotAuthorized
		}
		fmt.Println("lstk-ref: authorized")
		return 0
	case "signal-wait":
		return signalWait()
	default:
		return 0
	}
}

// signalWait reports signal delivery for lstk's signal-forwarding tests. It
// prints a readiness marker, then exits 40 + the number of SIGINT/SIGTERM
// received: 40 = none arrived before the deadline, 41 = exactly one, 42 = a
// double signal. After the first signal it lingers briefly so a
// near-simultaneous duplicate (the double-signal bug) is counted, not missed.
func signalWait() int {
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	fmt.Println("SIGNAL_WAIT_READY")

	count := 0
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-sigCh:
			count++
			if count >= 2 {
				return 40 + count
			}
			deadline = time.After(500 * time.Millisecond)
		case <-deadline:
			return 40 + count
		}
	}
}

// decodeContext decodes LSTK_EXT_CONTEXT, reporting a malformed payload but still
// returning the zero context so tests can observe the absence of fields.
func decodeContext() extContext {
	var c extContext
	if raw := os.Getenv("LSTK_EXT_CONTEXT"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			fmt.Fprintf(os.Stderr, "lstk-ref: invalid LSTK_EXT_CONTEXT: %v\n", err)
		}
	}
	return c
}

// echo prints the received args and decoded context in a stable, line-oriented
// form so integration tests can assert on individual fields.
func echo(args []string, c extContext) {
	fmt.Printf("ARGS=%v\n", args)
	if self, err := os.Executable(); err == nil {
		fmt.Printf("SELF=%s\n", self)
	}
	fmt.Printf("API_VERSION=%s\n", os.Getenv("LSTK_EXT_API_VERSION"))
	fmt.Printf("CONFIG_DIR=%s\n", c.ConfigDir)
	if c.AuthToken != "" {
		fmt.Printf("AUTH_TOKEN=%s\n", c.AuthToken)
	}
	fmt.Printf("NON_INTERACTIVE=%t\n", c.NonInteractive)
	fmt.Printf("JSON=%t\n", c.JSON)
	fmt.Printf("EMULATOR_COUNT=%d\n", len(c.Emulators))
	for _, e := range c.Emulators {
		fmt.Printf("EMULATOR=%s %s %s\n", e.Type, e.Endpoint, e.Port)
	}
}
