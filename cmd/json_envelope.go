package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/spf13/cobra"
)

// envelopeSinkKey is the context key jsonAwareSink stores an *output.EnvelopeSink
// under, so wrapCommandsWithJSONEnvelope can retrieve it after the command's RunE returns.
type envelopeSinkKey struct{}

func withEnvelopeSink(ctx context.Context, sink *output.EnvelopeSink) context.Context {
	return context.WithValue(ctx, envelopeSinkKey{}, sink)
}

func envelopeSinkFromContext(ctx context.Context) (*output.EnvelopeSink, bool) {
	sink, ok := ctx.Value(envelopeSinkKey{}).(*output.EnvelopeSink)
	return sink, ok
}

// jsonAwareSink returns the Sink a JSON-capable command's non-interactive path
// should use: an EnvelopeSink (registered on cmd's context for
// wrapCommandsWithJSONEnvelope to find once RunE returns) when --json is set,
// otherwise a plain PlainSink.
func jsonAwareSink(cmd *cobra.Command, cfg *env.Env, w io.Writer) output.Sink {
	if cfg.JSON {
		sink := output.NewEnvelopeSink(output.FormatJSON)
		cmd.SetContext(withEnvelopeSink(cmd.Context(), sink))
		return sink
	}
	return output.NewPlainSink(w)
}

// writeEnvelope marshals envelope as compact JSON and writes it to w, followed
// by a newline, as the single line of output a JSON-capable command produces.
func writeEnvelope(w io.Writer, envelope output.Envelope) error {
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// exitCodeFor maps an error envelope to the process exit code conventions
// documented in the output-envelope capability: 3 for CONFIRMATION_REQUIRED,
// 4 for AUTH_REQUIRED, 1 for every other error code, 0 when there is no error.
func exitCodeFor(envelope output.Envelope) int {
	if envelope.Error == nil {
		return 0
	}
	switch envelope.Error.Code {
	case output.ErrConfirmationRequired:
		return 3
	case output.ErrAuthRequired:
		return 4
	default:
		return 1
	}
}

// wrapCommandsWithJSONEnvelope walks the Cobra command tree and wraps every
// RunE so that, once a JSON-capable command's RunE returns while --json is
// set, the EnvelopeSink it registered via jsonAwareSink (if any) is finalized
// and written to stdout as exactly one JSON object, and the returned error is
// translated into the matching process exit code (see output.ExitCodeError).
// The wrapper is installed unconditionally; it only renders when --json was
// actually requested and a sink was registered — otherwise it's a no-op that
// passes the original error through untouched.
//
// A command rejected earlier by requireJSONSupport never reaches this wrapper
// with a registered sink — that rejection renders its own envelope directly,
// since there is no command-specific sink to build one from.
func wrapCommandsWithJSONEnvelope(cmd *cobra.Command, cfg *env.Env, stdout io.Writer) {
	walkCommandsWithRunE(cmd, func(c *cobra.Command) {
		original := c.RunE
		c.RunE = func(c *cobra.Command, args []string) error {
			runErr := original(c, args)

			if !cfg.JSON || isExtensionDispatch(c, args) {
				return runErr
			}

			sink, ok := envelopeSinkFromContext(c.Context())
			if !ok {
				return runErr
			}

			envelope := sink.Result(commandDisplayName(c), runErr)
			if writeErr := writeEnvelope(stdout, envelope); writeErr != nil {
				return writeErr
			}
			if runErr == nil {
				return nil
			}
			return output.NewSilentError(&output.ExitCodeError{Err: runErr, Code: exitCodeFor(envelope)})
		}
	})
}
