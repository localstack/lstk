package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// disableEventsEnv is the LocalStack-wide telemetry opt-out that extensions
// honour; it is not part of the LSTK_EXT_* contract.
const disableEventsEnv = "LOCALSTACK_DISABLE_EVENTS"

// quietWaitDelay bounds how long Wait drains stdout after the context ends: a
// grandchild still holding the pipe would otherwise block Wait past the
// caller's deadline (see awscli.completerWaitDelay).
const quietWaitDelay = 100 * time.Millisecond

// QuietOptions is what a caller of InvokeQuietlyWithJSON chooses. Everything
// else about the run is fixed by the helper; justify any field added here.
type QuietOptions struct {
	ConfigDir string
	// AuthToken is conveyed because extensions may refuse to run without it.
	AuthToken string
}

// InvokeQuietlyWithJSON runs ext on lstk's own behalf rather than the user's
// (the counterpart to Invoke) and returns the `data` of the single envelope it
// prints. It guarantees, regardless of the caller:
//   - the extension sees json: true and nonInteractive: true, and no session,
//     machine or endpoint id;
//   - LOCALSTACK_DISABLE_EVENTS=1, so the run is not reported as user activity;
//   - nil stdin, captured stdout and discarded stderr — nothing reaches the
//     user's terminal;
//   - a non-zero exit, unparseable output or a non-"ok" envelope is an error.
//
// It bypasses Invoke/proc.Run on purpose: this is a short captured-output
// exec, not a wrapped tool the user is driving, so there is no signal to
// forward. The working directory is inherited.
func InvokeQuietlyWithJSON(ctx context.Context, ext *Extension, args []string, opts QuietOptions) (json.RawMessage, error) {
	runCtx := Context{
		ConfigDir:      opts.ConfigDir,
		AuthToken:      opts.AuthToken,
		NonInteractive: true,
		JSON:           true,
	}
	envv, err := runCtx.Environ(os.Environ())
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, ext.Path, args...)
	if ext.Argv0 != "" {
		cmd.Args[0] = ext.Argv0
	}
	cmd.Env = append(envv, disableEventsEnv+"=1")
	cmd.WaitDelay = quietWaitDelay
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("extension %q: %w", ext.Name, err)
	}

	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		return nil, fmt.Errorf("extension %q: decode envelope: %w", ext.Name, err)
	}
	if envelope.Status != "ok" {
		if envelope.Error != nil {
			return nil, fmt.Errorf("extension %q: %s: %s", ext.Name, envelope.Error.Code, envelope.Error.Message)
		}
		return nil, errors.New("extension " + ext.Name + ": envelope status " + envelope.Status)
	}
	return envelope.Data, nil
}
